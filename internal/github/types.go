package github

import "time"

// Issue represents a GitHub issue.
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     string
	Author    string
	Labels    []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChangeType describes what changed on an issue.
type ChangeType int

const (
	ChangeNew           ChangeType = iota // Newly opened issue
	ChangeTitleEdited                     // Title was modified
	ChangeBodyEdited                      // Body was modified
	ChangeStateChanged                    // State changed (open/closed)
	ChangeLabelsChanged                   // Labels were added/removed
	ChangeOther                           // Other change
)

// String returns a human-readable name for the change type.
func (c ChangeType) String() string {
	switch c {
	case ChangeNew:
		return "new"
	case ChangeTitleEdited:
		return "title_edited"
	case ChangeBodyEdited:
		return "body_edited"
	case ChangeStateChanged:
		return "state_changed"
	case ChangeLabelsChanged:
		return "labels_changed"
	case ChangeOther:
		return "other"
	default:
		return "unknown"
	}
}

// IssueEvent is emitted when an issue is created or changed.
type IssueEvent struct {
	Repo       string
	Issue      Issue
	ChangeType ChangeType
}

// DuplicateCandidate is a potential duplicate issue with a similarity score.
type DuplicateCandidate struct {
	Number int
	Score  float32
}

// LabelSuggestion is a label suggestion with a confidence score.
type LabelSuggestion struct {
	Name       string
	Confidence float64
}

// TriageResult is the output of the triage pipeline for a single issue.
type TriageResult struct {
	Repo            string
	IssueNumber     int
	Duplicates      []DuplicateCandidate
	SuggestedLabels []LabelSuggestion
	Reasoning       string
}
