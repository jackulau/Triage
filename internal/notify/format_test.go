package notify

import (
	"testing"
	"time"

	"github.com/jacklau/triage/internal/github"
)

func TestFormatLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []github.LabelSuggestion
		want   string
	}{
		{
			name:   "empty",
			labels: nil,
			want:   "None",
		},
		{
			name: "single label",
			labels: []github.LabelSuggestion{
				{Name: "bug", Confidence: 0.94},
			},
			want: "`bug` (94%)",
		},
		{
			name: "multiple labels",
			labels: []github.LabelSuggestion{
				{Name: "bug", Confidence: 0.94},
				{Name: "crash", Confidence: 0.87},
			},
			want: "`bug` (94%), `crash` (87%)",
		},
		{
			name: "rounds correctly",
			labels: []github.LabelSuggestion{
				{Name: "enhancement", Confidence: 0.555},
			},
			want: "`enhancement` (56%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLabels(tt.labels)
			if got != tt.want {
				t.Errorf("FormatLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuplicates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []github.DuplicateCandidate
		want       string
	}{
		{
			name:       "empty",
			candidates: nil,
			want:       "None found",
		},
		{
			name: "single duplicate",
			candidates: []github.DuplicateCandidate{
				{Number: 38, Score: 0.91},
			},
			want: "- #38 — 91% similar",
		},
		{
			name: "multiple duplicates",
			candidates: []github.DuplicateCandidate{
				{Number: 38, Score: 0.91},
				{Number: 25, Score: 0.86},
			},
			want: "- #38 — 91% similar\n- #25 — 86% similar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuplicates(tt.candidates)
			if got != tt.want {
				t.Errorf("FormatDuplicates() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatConfidence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", "suggested"},
		{"High", "suggested"},
		{"medium", "possible"},
		{"Medium", "possible"},
		{"low", "uncertain"},
		{"Low", "uncertain"},
		{"", "uncertain"},
		{"unknown", "uncertain"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := FormatConfidence(tt.input)
			if got != tt.want {
				t.Errorf("FormatConfidence(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"just now", now, "just now"},
		{"seconds ago", now.Add(-30 * time.Second), "30 sec ago"},
		{"1 min ago", now.Add(-1 * time.Minute), "1 min ago"},
		{"2 min ago", now.Add(-2 * time.Minute), "2 min ago"},
		{"1 hour ago", now.Add(-1 * time.Hour), "1 hour ago"},
		{"3 hours ago", now.Add(-3 * time.Hour), "3 hours ago"},
		{"1 day ago", now.Add(-24 * time.Hour), "1 day ago"},
		{"5 days ago", now.Add(-5 * 24 * time.Hour), "5 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TimeAgo(tt.when)
			if got != tt.want {
				t.Errorf("TimeAgo() = %q, want %q", got, tt.want)
			}
		})
	}
}
