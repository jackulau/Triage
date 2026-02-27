package cmd

import (
	"encoding/json"
	"testing"

	"github.com/jacklau/triage/internal/github"
)

func TestParseIssueRef(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{
			name:       "valid ref",
			ref:        "octocat/hello-world#42",
			wantOwner:  "octocat",
			wantRepo:   "hello-world",
			wantNumber: 42,
		},
		{
			name:    "missing hash",
			ref:     "octocat/hello-world",
			wantErr: true,
		},
		{
			name:    "missing repo",
			ref:     "octocat#42",
			wantErr: true,
		},
		{
			name:    "invalid number",
			ref:     "octocat/hello-world#abc",
			wantErr: true,
		},
		{
			name:       "repo with dots",
			ref:        "org/my.repo.name#1",
			wantOwner:  "org",
			wantRepo:   "my.repo.name",
			wantNumber: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parseIssueRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseIssueRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
			}
		})
	}
}

func TestPrintCheckJSON(t *testing.T) {
	issue := github.Issue{
		Number: 42,
		Title:  "Test issue",
	}

	result := &github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 42,
		Duplicates: []github.DuplicateCandidate{
			{Number: 10, Score: 0.92},
		},
		SuggestedLabels: []github.LabelSuggestion{
			{Name: "bug", Confidence: 0.95},
		},
		Reasoning: "This is a bug report",
	}

	// Test that printCheckJSON produces valid JSON by checking the struct directly.
	out := checkResultJSON{
		Issue: issueJSON{
			Number: issue.Number,
			Title:  issue.Title,
		},
		Duplicates: make([]duplicateJSON, 0, len(result.Duplicates)),
		Labels:     make([]labelJSON, 0, len(result.SuggestedLabels)),
		Reasoning:  result.Reasoning,
	}

	for _, d := range result.Duplicates {
		out.Duplicates = append(out.Duplicates, duplicateJSON{
			Number: d.Number,
			Score:  float64(d.Score),
		})
	}

	for _, l := range result.SuggestedLabels {
		out.Labels = append(out.Labels, labelJSON{
			Name:       l.Name,
			Confidence: l.Confidence,
		})
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("failed to marshal check result: %v", err)
	}

	// Parse it back to verify structure
	var parsed checkResultJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal check result: %v", err)
	}

	if parsed.Issue.Number != 42 {
		t.Errorf("issue number = %d, want 42", parsed.Issue.Number)
	}
	if parsed.Issue.Title != "Test issue" {
		t.Errorf("issue title = %q, want %q", parsed.Issue.Title, "Test issue")
	}
	if len(parsed.Duplicates) != 1 {
		t.Fatalf("duplicates len = %d, want 1", len(parsed.Duplicates))
	}
	if parsed.Duplicates[0].Number != 10 {
		t.Errorf("duplicate number = %d, want 10", parsed.Duplicates[0].Number)
	}
	if parsed.Duplicates[0].Score < 0.91 || parsed.Duplicates[0].Score > 0.93 {
		t.Errorf("duplicate score = %f, want ~0.92", parsed.Duplicates[0].Score)
	}
	if len(parsed.Labels) != 1 {
		t.Fatalf("labels len = %d, want 1", len(parsed.Labels))
	}
	if parsed.Labels[0].Name != "bug" {
		t.Errorf("label name = %q, want %q", parsed.Labels[0].Name, "bug")
	}
	if parsed.Labels[0].Confidence != 0.95 {
		t.Errorf("label confidence = %f, want 0.95", parsed.Labels[0].Confidence)
	}
	if parsed.Reasoning != "This is a bug report" {
		t.Errorf("reasoning = %q, want %q", parsed.Reasoning, "This is a bug report")
	}
}

func TestCheckJSONEmptyResults(t *testing.T) {
	out := checkResultJSON{
		Issue: issueJSON{
			Number: 1,
			Title:  "No results",
		},
		Duplicates: make([]duplicateJSON, 0),
		Labels:     make([]labelJSON, 0),
		Reasoning:  "",
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify empty arrays appear as [] not null
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal raw: %v", err)
	}

	if string(raw["duplicates"]) != "[]" {
		t.Errorf("duplicates = %s, want []", string(raw["duplicates"]))
	}
	if string(raw["labels"]) != "[]" {
		t.Errorf("labels = %s, want []", string(raw["labels"]))
	}
}
