package notify

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jacklau/triage/internal/github"
)

// FormatLabels formats label suggestions as a readable string.
// Example: "`bug` (94%), `crash` (87%)"
func FormatLabels(labels []github.LabelSuggestion) string {
	if len(labels) == 0 {
		return "None"
	}
	parts := make([]string, len(labels))
	for i, l := range labels {
		pct := int(math.Round(l.Confidence * 100))
		parts[i] = fmt.Sprintf("`%s` (%d%%)", l.Name, pct)
	}
	return strings.Join(parts, ", ")
}

// FormatDuplicates formats duplicate candidates as a readable string.
// Example: "- #38 — 91% similar\n- #25 — 86% similar"
func FormatDuplicates(candidates []github.DuplicateCandidate) string {
	if len(candidates) == 0 {
		return "None found"
	}
	parts := make([]string, len(candidates))
	for i, d := range candidates {
		pct := int(math.Round(float64(d.Score) * 100))
		parts[i] = fmt.Sprintf("- #%d — %d%% similar", d.Number, pct)
	}
	return strings.Join(parts, "\n")
}

// FormatConfidence returns a human-readable confidence level.
func FormatConfidence(level string) string {
	switch strings.ToLower(level) {
	case "high":
		return "suggested"
	case "medium":
		return "possible"
	case "low":
		return "uncertain"
	default:
		return "uncertain"
	}
}

// TimeAgo returns a human-readable relative time string.
func TimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		secs := int(d.Seconds())
		if secs <= 1 {
			return "just now"
		}
		return fmt.Sprintf("%d sec ago", secs)
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
