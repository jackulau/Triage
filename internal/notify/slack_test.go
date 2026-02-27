package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestSlackNotifier_Notify_RetryOnError(t *testing.T) {
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

	notifier := NewSlackNotifier(server.URL)
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
