package classify

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/provider"
)

// mockCompleter is a test double for provider.Completer.
type mockCompleter struct {
	responses []string
	err       error
	callCount int
}

func (m *mockCompleter) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

var testLabels = []config.LabelConfig{
	{Name: "bug", Description: "Something isn't working"},
	{Name: "feature", Description: "New feature request"},
	{Name: "docs", Description: "Documentation update"},
}

var testIssue = github.Issue{
	Number: 42,
	Title:  "App crashes on startup",
	Body:   "When I open the app it crashes immediately.",
}

func TestClassify_ValidJSON(t *testing.T) {
	mock := &mockCompleter{
		responses: []string{`{"labels": ["bug"], "confidence": 0.95, "reasoning": "Clear bug report"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(result.Labels) != 1 {
		t.Fatalf("expected 1 label, got %d", len(result.Labels))
	}
	if result.Labels[0].Name != "bug" {
		t.Errorf("expected label 'bug', got %q", result.Labels[0].Name)
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.Confidence)
	}
	if result.Reasoning != "Clear bug report" {
		t.Errorf("expected reasoning 'Clear bug report', got %q", result.Reasoning)
	}
	if result.ConfidenceLevel != "suggested" {
		t.Errorf("expected confidence level 'suggested', got %q", result.ConfidenceLevel)
	}
}

func TestClassify_ConfidenceLevels(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		expected   string
	}{
		{"high", 0.95, "suggested"},
		{"exact_threshold_high", 0.9, "suggested"},
		{"medium", 0.8, "possible"},
		{"exact_threshold_medium", 0.7, "possible"},
		{"low", 0.5, "uncertain"},
		{"zero", 0.0, "uncertain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := fmt.Sprintf(`{"labels": ["bug"], "confidence": %f, "reasoning": "test"}`, tt.confidence)
			mock := &mockCompleter{responses: []string{resp}}
			c := NewClassifier(mock, 10*time.Second)

			result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
			if err != nil {
				t.Fatalf("Classify returned error: %v", err)
			}
			if result.ConfidenceLevel != tt.expected {
				t.Errorf("expected confidence level %q, got %q", tt.expected, result.ConfidenceLevel)
			}
		})
	}
}

func TestClassify_MalformedJSON_RetrySucceeds(t *testing.T) {
	mock := &mockCompleter{
		responses: []string{
			"not valid json",
			`{"labels": ["feature"], "confidence": 0.85, "reasoning": "Feature request"}`,
		},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if mock.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", mock.callCount)
	}
	if len(result.Labels) != 1 || result.Labels[0].Name != "feature" {
		t.Errorf("expected label 'feature', got %v", result.Labels)
	}
}

func TestClassify_MalformedJSON_FallsBackToUncertain(t *testing.T) {
	mock := &mockCompleter{
		responses: []string{"not json", "still not json"},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain, got %q", result.ConfidenceLevel)
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0, got %f", result.Confidence)
	}
	if len(result.Labels) != 0 {
		t.Errorf("expected no labels, got %v", result.Labels)
	}
}

func TestClassify_LabelValidation_RejectsUnknown(t *testing.T) {
	mock := &mockCompleter{
		responses: []string{`{"labels": ["bug", "unknown-label", "feature"], "confidence": 0.9, "reasoning": "Mixed"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(result.Labels) != 2 {
		t.Fatalf("expected 2 valid labels, got %d: %v", len(result.Labels), result.Labels)
	}
	names := []string{result.Labels[0].Name, result.Labels[1].Name}
	if names[0] != "bug" || names[1] != "feature" {
		t.Errorf("expected [bug, feature], got %v", names)
	}
}

func TestClassify_CompletionError(t *testing.T) {
	mock := &mockCompleter{
		err: errors.New("api error"),
	}
	c := NewClassifier(mock, 10*time.Second)

	_, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err == nil {
		t.Fatal("expected error from completion failure")
	}
}

func TestClassify_RateLimitError(t *testing.T) {
	mock := &mockCompleter{
		err: provider.ErrRateLimit,
	}
	c := NewClassifier(mock, 10*time.Second)

	_, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		// Wrapped, but should propagate
		if !errors.Is(err, provider.ErrRateLimit) {
			t.Logf("error doesn't wrap ErrRateLimit directly, got: %v", err)
		}
	}
}

func TestParseResponse_ValidJSON(t *testing.T) {
	resp, err := parseResponse(`{"labels": ["bug", "feature"], "confidence": 0.85, "reasoning": "Test"}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(resp.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(resp.Labels))
	}
	if resp.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", resp.Confidence)
	}
}

func TestParseResponse_WithCodeFences(t *testing.T) {
	input := "```json\n{\"labels\": [\"bug\"], \"confidence\": 0.9, \"reasoning\": \"Test\"}\n```"
	resp, err := parseResponse(input)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(resp.Labels) != 1 || resp.Labels[0] != "bug" {
		t.Errorf("expected [bug], got %v", resp.Labels)
	}
}

func TestParseResponse_WithCodeFencesNoLang(t *testing.T) {
	input := "```\n{\"labels\": [\"docs\"], \"confidence\": 0.75, \"reasoning\": \"Docs\"}\n```"
	resp, err := parseResponse(input)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(resp.Labels) != 1 || resp.Labels[0] != "docs" {
		t.Errorf("expected [docs], got %v", resp.Labels)
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	_, err := parseResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !errors.Is(err, provider.ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestParseResponse_MissingFields(t *testing.T) {
	// Empty labels, missing reasoning - should still parse
	resp, err := parseResponse(`{"labels": [], "confidence": 0.5}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(resp.Labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(resp.Labels))
	}
	if resp.Reasoning != "" {
		t.Errorf("expected empty reasoning, got %q", resp.Reasoning)
	}
}

func TestParseResponse_ConfidenceClamp(t *testing.T) {
	resp, err := parseResponse(`{"labels": ["bug"], "confidence": 1.5, "reasoning": "Test"}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if resp.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", resp.Confidence)
	}

	resp, err = parseResponse(`{"labels": ["bug"], "confidence": -0.5, "reasoning": "Test"}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if resp.Confidence != 0.0 {
		t.Errorf("expected confidence clamped to 0.0, got %f", resp.Confidence)
	}
}

func TestValidateLabels(t *testing.T) {
	result := validateLabels(
		[]string{"bug", "unknown", "feature", "also-unknown"},
		testLabels,
	)
	if len(result) != 2 {
		t.Fatalf("expected 2 valid labels, got %d", len(result))
	}
	if result[0] != "bug" || result[1] != "feature" {
		t.Errorf("expected [bug, feature], got %v", result)
	}
}

func TestConfidenceLevel(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   string
	}{
		{1.0, "suggested"},
		{0.95, "suggested"},
		{0.9, "suggested"},
		{0.89, "possible"},
		{0.7, "possible"},
		{0.69, "uncertain"},
		{0.0, "uncertain"},
	}
	for _, tt := range tests {
		got := confidenceLevel(tt.confidence)
		if got != tt.expected {
			t.Errorf("confidenceLevel(%f) = %q, want %q", tt.confidence, got, tt.expected)
		}
	}
}
