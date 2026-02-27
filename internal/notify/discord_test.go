package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestDiscordNotifier_Notify_RetryOnError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewDiscordNotifier(server.URL)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	err := notifier.Notify(context.Background(), result)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
