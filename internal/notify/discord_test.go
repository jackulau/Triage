package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jacklau/triage/internal/github"
)

func TestBuildDiscordPayload_Structure(t *testing.T) {
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 42,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "bug", Confidence: 0.94},
			{Name: "crash", Confidence: 0.87},
		},
		Duplicates: []github.DuplicateCandidate{
			{Number: 38, Score: 0.91},
		},
		Reasoning: "This appears to be a crash bug related to startup.",
	}

	payload := BuildDiscordPayload(result)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	embeds, ok := parsed["embeds"].([]interface{})
	if !ok || len(embeds) != 1 {
		t.Fatal("expected exactly 1 embed")
	}

	embed := embeds[0].(map[string]interface{})

	// Title
	title := embed["title"].(string)
	if title != "#42" {
		t.Errorf("expected title '#42', got %q", title)
	}

	// URL
	url := embed["url"].(string)
	if url != "https://github.com/owner/repo/issues/42" {
		t.Errorf("unexpected URL: %q", url)
	}

	// Color
	color := int(embed["color"].(float64))
	if color != 15158332 {
		t.Errorf("expected color 15158332, got %d", color)
	}

	// Fields
	fields := embed["fields"].([]interface{})
	if len(fields) != 3 { // Labels, Duplicates, Reasoning
		t.Errorf("expected 3 fields, got %d", len(fields))
	}

	// Labels field
	labelsField := fields[0].(map[string]interface{})
	if labelsField["name"] != "Labels" {
		t.Errorf("expected Labels field, got %q", labelsField["name"])
	}
	if labelsField["inline"] != true {
		t.Error("expected Labels field to be inline")
	}

	// Duplicates field
	dupsField := fields[1].(map[string]interface{})
	if dupsField["name"] != "Duplicates" {
		t.Errorf("expected Duplicates field, got %q", dupsField["name"])
	}
	if dupsField["inline"] != true {
		t.Error("expected Duplicates field to be inline")
	}

	// Reasoning field
	reasonField := fields[2].(map[string]interface{})
	if reasonField["name"] != "Reasoning" {
		t.Errorf("expected Reasoning field, got %q", reasonField["name"])
	}
	if reasonField["inline"] != false {
		t.Error("expected Reasoning field to not be inline")
	}

	// Footer
	footer := embed["footer"].(map[string]interface{})
	if footer["text"] != "triage - owner/repo" {
		t.Errorf("unexpected footer text: %q", footer["text"])
	}
}

func TestBuildDiscordPayload_NoReasoning(t *testing.T) {
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 10,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "enhancement", Confidence: 0.75},
		},
	}

	payload := BuildDiscordPayload(result)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	embeds := parsed["embeds"].([]interface{})
	embed := embeds[0].(map[string]interface{})
	fields := embed["fields"].([]interface{})

	// Labels + Duplicates = 2 (no Reasoning)
	if len(fields) != 2 {
		t.Errorf("expected 2 fields without reasoning, got %d", len(fields))
	}
}

func TestDiscordNotifier_Notify_Success(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		var err error
		receivedBody = make([]byte, r.ContentLength)
		_, err = r.Body.Read(receivedBody)
		if err != nil && err.Error() != "EOF" {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier(server.URL)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "bug", Confidence: 0.9},
		},
	}

	err := notifier.Notify(context.Background(), result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(receivedBody) == 0 {
		t.Error("expected non-empty request body")
	}
}

func TestDiscordNotifier_Notify_HTTPError(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad gateway"))
	}))
	defer server.Close()

	notifier := NewDiscordNotifier(server.URL)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	err := notifier.Notify(context.Background(), result)
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}

	// Notifier should be called exactly once (no internal retry; callers handle retry)
	if got := callCount.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
}

func TestDiscordNotifier_Notify_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a very slow response
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &DiscordNotifier{
		webhookURL: server.URL,
		client: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
	}

	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	err := notifier.Notify(context.Background(), result)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDiscordNotifier_Notify_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier(server.URL)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := notifier.Notify(ctx, result)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestDiscordNotifier_Notify_VerifiesRequestBodyJSON(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	var gotMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier(server.URL)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 42,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "bug", Confidence: 0.94},
		},
		Duplicates: []github.DuplicateCandidate{
			{Number: 10, Score: 0.88},
		},
		Reasoning: "Looks like a bug",
	}

	err := notifier.Notify(context.Background(), result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify HTTP method
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST method, got %q", gotMethod)
	}

	// Verify Content-Type
	if gotContentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", gotContentType)
	}

	// Verify the body is valid JSON with correct Discord embed structure
	var payload discordPayload
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not valid discord payload JSON: %v", err)
	}

	if len(payload.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
	}

	embed := payload.Embeds[0]
	if embed.Title != "#42" {
		t.Errorf("expected title '#42', got %q", embed.Title)
	}
	// Should have 3 fields: Labels, Duplicates, Reasoning
	if len(embed.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(embed.Fields))
	}
	if embed.Footer == nil {
		t.Error("expected non-nil footer")
	}
}

func TestDiscordNotifier_ClientTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	notifier := NewDiscordNotifier("http://example.com")
	if notifier.client.Timeout != 30*time.Second {
		t.Errorf("expected client timeout of 30s, got %v", notifier.client.Timeout)
	}
}

func TestDiscordNotifier_Notify_TimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	// Server that delays longer than the client timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create notifier with a very short timeout for test speed
	notifier := &DiscordNotifier{
		webhookURL: server.URL,
		client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	}

	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "bug", Confidence: 0.9},
		},
	}

	err := notifier.Notify(context.Background(), result)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// The error should indicate a timeout or deadline exceeded
	errStr := err.Error()
	if !strings.Contains(errStr, "Client.Timeout") && !strings.Contains(errStr, "deadline exceeded") && !strings.Contains(errStr, "context deadline") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}
