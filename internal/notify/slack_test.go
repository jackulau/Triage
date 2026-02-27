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

func TestBuildSlackPayload_Structure(t *testing.T) {
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

	payload := BuildSlackPayload(result)

	// Marshal to JSON and parse back to verify structure
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	blocks, ok := parsed["blocks"].([]interface{})
	if !ok {
		t.Fatal("expected blocks array")
	}

	// Header block
	if len(blocks) < 1 {
		t.Fatal("expected at least 1 block")
	}
	header := blocks[0].(map[string]interface{})
	if header["type"] != "header" {
		t.Errorf("expected header block, got %q", header["type"])
	}
	headerText := header["text"].(map[string]interface{})
	if headerText["text"] != "New Issue Needs Triage" {
		t.Errorf("unexpected header text: %v", headerText["text"])
	}

	// Issue link block
	issueBlock := blocks[1].(map[string]interface{})
	if issueBlock["type"] != "section" {
		t.Errorf("expected section block, got %q", issueBlock["type"])
	}
	issueTxt := issueBlock["text"].(map[string]interface{})
	issueStr := issueTxt["text"].(string)
	if issueStr == "" {
		t.Error("expected issue link text")
	}

	// Labels block
	labelsBlock := blocks[2].(map[string]interface{})
	labelsTxt := labelsBlock["text"].(map[string]interface{})
	labelsStr := labelsTxt["text"].(string)
	if labelsStr == "" {
		t.Error("expected labels text")
	}

	// Duplicates block (should exist since we have duplicates)
	if len(blocks) < 4 {
		t.Fatal("expected duplicates block")
	}
	dupsBlock := blocks[3].(map[string]interface{})
	dupsTxt := dupsBlock["text"].(map[string]interface{})
	dupsStr := dupsTxt["text"].(string)
	if dupsStr == "" {
		t.Error("expected duplicates text")
	}

	// Reasoning block
	if len(blocks) < 5 {
		t.Fatal("expected reasoning block")
	}
	reasonBlock := blocks[4].(map[string]interface{})
	reasonTxt := reasonBlock["text"].(map[string]interface{})
	reasonStr := reasonTxt["text"].(string)
	if reasonStr == "" {
		t.Error("expected reasoning text")
	}
}

func TestBuildSlackPayload_NoDuplicates(t *testing.T) {
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 10,
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "enhancement", Confidence: 0.75},
		},
		Reasoning: "Feature request.",
	}

	payload := BuildSlackPayload(result)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	blocks := parsed["blocks"].([]interface{})
	// header + issue + labels + reasoning = 4 (no duplicates block)
	if len(blocks) != 4 {
		t.Errorf("expected 4 blocks without duplicates, got %d", len(blocks))
	}
}

func TestSlackNotifier_Notify_Success(t *testing.T) {
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

	notifier := NewSlackNotifier(server.URL)
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

func TestSlackNotifier_Notify_HTTPError(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad gateway"))
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL)
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

func TestSlackNotifier_Notify_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a very slow response
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &SlackNotifier{
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

func TestSlackNotifier_Notify_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL)
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

func TestSlackNotifier_Notify_VerifiesRequestBodyJSON(t *testing.T) {
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

	notifier := NewSlackNotifier(server.URL)
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

	// Verify the body is valid JSON with correct structure
	var payload slackPayload
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not valid slack payload JSON: %v", err)
	}

	// Should have 5 blocks: header, issue link, labels, duplicates, reasoning
	if len(payload.Blocks) != 5 {
		t.Errorf("expected 5 blocks, got %d", len(payload.Blocks))
	}
}

func TestSlackNotifier_ClientTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	notifier := NewSlackNotifier("http://example.com")
	if notifier.client.Timeout != 30*time.Second {
		t.Errorf("expected client timeout of 30s, got %v", notifier.client.Timeout)
	}
}

func TestSlackNotifier_Notify_TimesOut(t *testing.T) {
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
	notifier := &SlackNotifier{
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
