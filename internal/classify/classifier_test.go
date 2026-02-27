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

// --- Error path tests ---

func TestClassify_ContextCancellation(t *testing.T) {
	// The completer will never respond because context is already cancelled
	mock := &mockCompleter{
		err: context.Canceled,
	}
	c := NewClassifier(mock, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.Classify(ctx, "owner/repo", testLabels, testIssue)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClassify_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockCompleter{
		err: context.DeadlineExceeded,
	}
	c := NewClassifier(mock, 10*time.Second)

	_, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err == nil {
		t.Fatal("expected error for deadline exceeded")
	}
}

func TestClassify_EmptyLabelsResponse(t *testing.T) {
	// LLM returns valid JSON but with no labels
	mock := &mockCompleter{
		responses: []string{`{"labels": [], "confidence": 0.3, "reasoning": "Cannot determine category"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(result.Labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(result.Labels))
	}
	if result.Confidence != 0.3 {
		t.Errorf("expected confidence 0.3, got %f", result.Confidence)
	}
	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain, got %q", result.ConfidenceLevel)
	}
}

func TestClassify_PartialJSON_TruncatedResponse(t *testing.T) {
	// LLM returns truncated JSON (e.g., context length limit)
	mock := &mockCompleter{
		responses: []string{
			`{"labels": ["bug"], "confide`,            // truncated first attempt
			`{"labels": ["bug"], "confidence": 0.8, `, // truncated retry
		},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	// Both attempts fail to parse, should fall back to uncertain
	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain for truncated JSON, got %q", result.ConfidenceLevel)
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0, got %f", result.Confidence)
	}
	if len(result.Labels) != 0 {
		t.Errorf("expected no labels, got %v", result.Labels)
	}
}

func TestClassify_NegativeConfidence(t *testing.T) {
	// LLM returns negative confidence, should be clamped to 0
	mock := &mockCompleter{
		responses: []string{`{"labels": ["bug"], "confidence": -0.5, "reasoning": "Negative confidence"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if result.Confidence != 0.0 {
		t.Errorf("expected confidence clamped to 0.0, got %f", result.Confidence)
	}
	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain for clamped confidence, got %q", result.ConfidenceLevel)
	}
}

func TestClassify_ConfidenceAboveOne(t *testing.T) {
	// LLM returns confidence > 1.0, should be clamped to 1.0
	mock := &mockCompleter{
		responses: []string{`{"labels": ["bug"], "confidence": 2.5, "reasoning": "Overly confident"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if result.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", result.Confidence)
	}
	if result.ConfidenceLevel != "suggested" {
		t.Errorf("expected suggested for max confidence, got %q", result.ConfidenceLevel)
	}
}

func TestClassify_AllLabelsUnknown(t *testing.T) {
	// LLM returns only labels not in the configured set
	mock := &mockCompleter{
		responses: []string{`{"labels": ["security", "performance", "networking"], "confidence": 0.9, "reasoning": "Wrong labels"}`},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	// All labels rejected by validation
	if len(result.Labels) != 0 {
		t.Errorf("expected 0 valid labels, got %d: %v", len(result.Labels), result.Labels)
	}
	// Confidence and reasoning are still from the LLM response
	if result.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", result.Confidence)
	}
}

func TestClassify_LLMReturnsInstructions(t *testing.T) {
	// LLM returns conversational text instead of JSON
	instructions := `Sure! I'd be happy to help classify this issue.
Based on the description, this appears to be a bug report.
The user is experiencing a crash on startup which is clearly a defect.

I would label this as "bug" with high confidence.`

	mock := &mockCompleter{
		responses: []string{
			instructions,
			instructions, // retry also returns instructions
		},
	}
	c := NewClassifier(mock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	// Both attempts should fail to parse, falling back to uncertain
	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain for instructions response, got %q", result.ConfidenceLevel)
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0, got %f", result.Confidence)
	}
	if len(result.Labels) != 0 {
		t.Errorf("expected no labels, got %v", result.Labels)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", mock.callCount)
	}
}

func TestClassify_RetryErrorFallsBackToUncertain(t *testing.T) {
	// First call returns unparseable JSON, retry returns an error
	callCount := 0
	errMock := &errOnRetryCompleter{
		firstResponse: "not valid json",
		retryErr:      errors.New("connection reset"),
		callCount:     &callCount,
	}
	c := NewClassifier(errMock, 10*time.Second)

	result, err := c.Classify(context.Background(), "owner/repo", testLabels, testIssue)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if result.ConfidenceLevel != "uncertain" {
		t.Errorf("expected uncertain, got %q", result.ConfidenceLevel)
	}
	if result.Reasoning != "Failed to get valid response from LLM" {
		t.Errorf("expected fallback reasoning, got %q", result.Reasoning)
	}
}

// errOnRetryCompleter returns a response on first call and an error on retry.
type errOnRetryCompleter struct {
	firstResponse string
	retryErr      error
	callCount     *int
}

func (m *errOnRetryCompleter) Complete(_ context.Context, _ string) (string, error) {
	*m.callCount++
	if *m.callCount == 1 {
		return m.firstResponse, nil
	}
	return "", m.retryErr
}

func TestParseResponse_TruncatedJSON(t *testing.T) {
	truncated := `{"labels": ["bug"], "confidence": 0.9, "reasonin`
	_, err := parseResponse(truncated)
	if err == nil {
		t.Fatal("expected error for truncated JSON")
	}
	if !errors.Is(err, provider.ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestParseResponse_InstructionsText(t *testing.T) {
	instructions := `Based on my analysis, this is clearly a bug report. I recommend labeling it as "bug".`
	_, err := parseResponse(instructions)
	if err == nil {
		t.Fatal("expected error for instructions text")
	}
	if !errors.Is(err, provider.ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestParseResponse_EmptyString(t *testing.T) {
	_, err := parseResponse("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
	if !errors.Is(err, provider.ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestParseResponse_NegativeConfidenceClamp(t *testing.T) {
	resp, err := parseResponse(`{"labels": ["bug"], "confidence": -100.0, "reasoning": "Very negative"}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if resp.Confidence != 0.0 {
		t.Errorf("expected confidence clamped to 0.0, got %f", resp.Confidence)
	}
}

func TestParseResponse_HighConfidenceClamp(t *testing.T) {
	resp, err := parseResponse(`{"labels": ["bug"], "confidence": 999.0, "reasoning": "Extremely confident"}`)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if resp.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", resp.Confidence)
	}
}

func TestParseResponse_JSONWithExtraWhitespace(t *testing.T) {
	input := `

	{"labels": ["bug"], "confidence": 0.8, "reasoning": "Test"}

	`
	resp, err := parseResponse(input)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(resp.Labels) != 1 || resp.Labels[0] != "bug" {
		t.Errorf("expected [bug], got %v", resp.Labels)
	}
}

func TestValidateLabels_AllUnknown(t *testing.T) {
	result := validateLabels(
		[]string{"security", "performance", "networking"},
		testLabels,
	)
	if len(result) != 0 {
		t.Errorf("expected 0 valid labels, got %d: %v", len(result), result)
	}
}

func TestValidateLabels_Empty(t *testing.T) {
	result := validateLabels([]string{}, testLabels)
	if len(result) != 0 {
		t.Errorf("expected 0 labels, got %d", len(result))
	}
}

func TestValidateLabels_NilInput(t *testing.T) {
	result := validateLabels(nil, testLabels)
	if len(result) != 0 {
		t.Errorf("expected 0 labels, got %d", len(result))
	}
}
