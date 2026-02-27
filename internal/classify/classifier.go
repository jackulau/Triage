package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/provider"
)

// Classifier uses an LLM completer to classify GitHub issues.
type Classifier struct {
	completer provider.Completer
	timeout   time.Duration
}

// ClassifyResult holds the output of issue classification.
type ClassifyResult struct {
	Labels          []github.LabelSuggestion
	Confidence      float64
	Reasoning       string
	ConfidenceLevel string // "suggested", "possible", or "uncertain"
}

// NewClassifier creates a new Classifier with the given completer and timeout.
// If timeout is zero, defaults to 30 seconds.
func NewClassifier(completer provider.Completer, timeout time.Duration) *Classifier {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Classifier{
		completer: completer,
		timeout:   timeout,
	}
}

// llmResponse is the expected JSON structure from the LLM.
type llmResponse struct {
	Labels     []string `json:"labels"`
	Confidence float64  `json:"confidence"`
	Reasoning  string   `json:"reasoning"`
}

// codeFenceRe matches markdown code fences around JSON.
var codeFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")

// parseResponse parses the LLM's JSON response, stripping markdown fences if present.
func parseResponse(raw string) (*llmResponse, error) {
	cleaned := strings.TrimSpace(raw)

	// Strip markdown code fences if present
	if matches := codeFenceRe.FindStringSubmatch(cleaned); len(matches) > 1 {
		cleaned = strings.TrimSpace(matches[1])
	}

	var resp llmResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("%w: %s", provider.ErrInvalidResponse, err)
	}

	// Validate confidence is in [0, 1]
	if resp.Confidence < 0 {
		resp.Confidence = 0
	}
	if resp.Confidence > 1 {
		resp.Confidence = 1
	}

	return &resp, nil
}

// confidenceLevel returns the confidence level string based on the confidence value.
func confidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "suggested"
	case confidence >= 0.7:
		return "possible"
	default:
		return "uncertain"
	}
}

// validateLabels filters the returned labels against the configured label set,
// rejecting any unknown labels.
func validateLabels(returned []string, configured []config.LabelConfig) []string {
	valid := make(map[string]bool, len(configured))
	for _, lc := range configured {
		valid[lc.Name] = true
	}

	var result []string
	for _, name := range returned {
		if valid[name] {
			result = append(result, name)
		}
	}
	return result
}

const retryPromptSuffix = `

IMPORTANT: You MUST respond with ONLY valid JSON. No markdown, no code fences, no extra text.
Example: {"labels": ["bug"], "confidence": 0.8, "reasoning": "This is a bug report"}`

// Classify classifies a GitHub issue using the LLM completer.
func (c *Classifier) Classify(ctx context.Context, repo string, labels []config.LabelConfig, issue github.Issue) (*ClassifyResult, error) {
	return c.ClassifyWithCustomPrompt(ctx, repo, labels, issue, "")
}

// ClassifyWithCustomPrompt classifies a GitHub issue using the LLM completer,
// appending customPrompt as additional context when non-empty.
func (c *Classifier) ClassifyWithCustomPrompt(ctx context.Context, repo string, labels []config.LabelConfig, issue github.Issue, customPrompt string) (*ClassifyResult, error) {
	prompt, err := BuildPromptWithCustom(repo, labels, issue, customPrompt)
	if err != nil {
		return nil, fmt.Errorf("building prompt: %w", err)
	}

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// First attempt
	raw, err := c.completer.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("completing prompt: %w", err)
	}

	resp, err := parseResponse(raw)
	if err != nil {
		// Retry once with stricter prompt
		retryPrompt := prompt + retryPromptSuffix
		raw, retryErr := c.completer.Complete(ctx, retryPrompt)
		if retryErr != nil {
			// Fall back to uncertain
			return &ClassifyResult{
				Labels:          nil,
				Confidence:      0,
				Reasoning:       "Failed to get valid response from LLM",
				ConfidenceLevel: "uncertain",
			}, nil
		}

		resp, err = parseResponse(raw)
		if err != nil {
			// Fall back to uncertain
			return &ClassifyResult{
				Labels:          nil,
				Confidence:      0,
				Reasoning:       "Failed to parse LLM response after retry",
				ConfidenceLevel: "uncertain",
			}, nil
		}
	}

	// Validate labels against configured set
	validLabels := validateLabels(resp.Labels, labels)

	// Build label suggestions
	suggestions := make([]github.LabelSuggestion, len(validLabels))
	for i, name := range validLabels {
		suggestions[i] = github.LabelSuggestion{
			Name:       name,
			Confidence: resp.Confidence,
		}
	}

	return &ClassifyResult{
		Labels:          suggestions,
		Confidence:      resp.Confidence,
		Reasoning:       resp.Reasoning,
		ConfidenceLevel: confidenceLevel(resp.Confidence),
	}, nil
}
