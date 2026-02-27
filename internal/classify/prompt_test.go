package classify

import (
	"strings"
	"testing"

	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/github"
)

func TestBuildPrompt_RendersAllVariables(t *testing.T) {
	labels := []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature request"},
	}
	issue := github.Issue{
		Number: 42,
		Title:  "App crashes on startup",
		Body:   "When I open the app it crashes immediately.",
	}

	prompt, err := BuildPrompt("owner/repo", labels, issue)
	if err != nil {
		t.Fatalf("BuildPrompt returned error: %v", err)
	}

	// Check repo name
	if !strings.Contains(prompt, "owner/repo") {
		t.Error("prompt does not contain repo name")
	}

	// Check labels
	if !strings.Contains(prompt, "bug: Something isn't working") {
		t.Error("prompt does not contain bug label")
	}
	if !strings.Contains(prompt, "feature: New feature request") {
		t.Error("prompt does not contain feature label")
	}

	// Check issue number
	if !strings.Contains(prompt, "#42") {
		t.Error("prompt does not contain issue number")
	}

	// Check issue title
	if !strings.Contains(prompt, "App crashes on startup") {
		t.Error("prompt does not contain issue title")
	}

	// Check issue body
	if !strings.Contains(prompt, "When I open the app it crashes immediately.") {
		t.Error("prompt does not contain issue body")
	}

	// Check JSON example format
	if !strings.Contains(prompt, `"labels"`) {
		t.Error("prompt does not contain JSON format example")
	}
}

func TestBuildPrompt_EmptyRepo(t *testing.T) {
	labels := []config.LabelConfig{{Name: "bug", Description: "Bug"}}
	issue := github.Issue{Number: 1, Title: "Test"}

	_, err := BuildPrompt("", labels, issue)
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
}

func TestBuildPrompt_NoLabels(t *testing.T) {
	issue := github.Issue{Number: 1, Title: "Test"}

	_, err := BuildPrompt("owner/repo", nil, issue)
	if err == nil {
		t.Fatal("expected error for no labels")
	}
}

func TestBuildPrompt_EmptyIssueBody(t *testing.T) {
	labels := []config.LabelConfig{{Name: "bug", Description: "Bug"}}
	issue := github.Issue{Number: 1, Title: "Test", Body: ""}

	prompt, err := BuildPrompt("owner/repo", labels, issue)
	if err != nil {
		t.Fatalf("BuildPrompt returned error: %v", err)
	}

	if !strings.Contains(prompt, "#1") {
		t.Error("prompt does not contain issue number")
	}
}

func TestBuildPromptWithCustom_AppendsCustomPrompt(t *testing.T) {
	labels := []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
	}
	issue := github.Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "Test body",
	}

	customPrompt := "This repo uses a monorepo structure. Focus on backend issues."

	prompt, err := BuildPromptWithCustom("owner/repo", labels, issue, customPrompt)
	if err != nil {
		t.Fatalf("BuildPromptWithCustom returned error: %v", err)
	}

	// Verify custom prompt is appended
	if !strings.Contains(prompt, "Additional context:") {
		t.Error("prompt does not contain 'Additional context:' section")
	}
	if !strings.Contains(prompt, customPrompt) {
		t.Errorf("prompt does not contain custom prompt %q", customPrompt)
	}

	// Verify it comes after the main prompt content
	mainIdx := strings.Index(prompt, `"labels"`)
	customIdx := strings.Index(prompt, customPrompt)
	if mainIdx == -1 || customIdx == -1 || customIdx < mainIdx {
		t.Error("custom prompt should appear after the main prompt template")
	}
}

func TestBuildPromptWithCustom_EmptyCustomPrompt(t *testing.T) {
	labels := []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
	}
	issue := github.Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "Test body",
	}

	prompt, err := BuildPromptWithCustom("owner/repo", labels, issue, "")
	if err != nil {
		t.Fatalf("BuildPromptWithCustom returned error: %v", err)
	}

	// Verify "Additional context:" is NOT included when custom prompt is empty
	if strings.Contains(prompt, "Additional context:") {
		t.Error("prompt should not contain 'Additional context:' when custom prompt is empty")
	}

	// Should produce the same result as BuildPrompt
	basePrompt, err := BuildPrompt("owner/repo", labels, issue)
	if err != nil {
		t.Fatalf("BuildPrompt returned error: %v", err)
	}
	if prompt != basePrompt {
		t.Error("BuildPromptWithCustom with empty custom prompt should equal BuildPrompt")
	}
}

func TestBuildPromptWithCustom_ValidationErrors(t *testing.T) {
	labels := []config.LabelConfig{{Name: "bug", Description: "Bug"}}
	issue := github.Issue{Number: 1, Title: "Test"}

	// Empty repo
	_, err := BuildPromptWithCustom("", labels, issue, "custom")
	if err == nil {
		t.Error("expected error for empty repo")
	}

	// No labels
	_, err = BuildPromptWithCustom("owner/repo", nil, issue, "custom")
	if err == nil {
		t.Error("expected error for no labels")
	}
}
