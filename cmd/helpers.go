package cmd

import (
	"github.com/jacklau/triage/internal/github"

	gogithub "github.com/google/go-github/v60/github"
)

// convertGHIssue converts a go-github Issue pointer to our internal github.Issue type.
// This is the single shared conversion function used by scan.go and check.go.
func convertGHIssue(gh *gogithub.Issue) github.Issue {
	issue := github.Issue{
		Number: gh.GetNumber(),
		Title:  gh.GetTitle(),
		Body:   gh.GetBody(),
		State:  gh.GetState(),
	}
	if gh.User != nil {
		issue.Author = gh.User.GetLogin()
	}
	for _, label := range gh.Labels {
		issue.Labels = append(issue.Labels, label.GetName())
	}
	if gh.CreatedAt != nil {
		issue.CreatedAt = gh.CreatedAt.Time
	}
	if gh.UpdatedAt != nil {
		issue.UpdatedAt = gh.UpdatedAt.Time
	}
	return issue
}
