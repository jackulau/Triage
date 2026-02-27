package cmd

import (
	"testing"
	"time"

	gogithub "github.com/google/go-github/v60/github"
)

func TestConvertGHIssue(t *testing.T) {
	t.Run("converts all fields", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		created := gogithub.Timestamp{Time: now.Add(-time.Hour)}
		updated := gogithub.Timestamp{Time: now}

		gh := &gogithub.Issue{
			Number: intPtr(42),
			Title:  strPtr("Test Issue"),
			Body:   strPtr("This is the body"),
			State:  strPtr("open"),
			User:   &gogithub.User{Login: strPtr("octocat")},
			Labels: []*gogithub.Label{
				{Name: strPtr("bug")},
				{Name: strPtr("help wanted")},
			},
			CreatedAt: &created,
			UpdatedAt: &updated,
		}

		issue := convertGHIssue(gh)

		if issue.Number != 42 {
			t.Errorf("Number = %d, want 42", issue.Number)
		}
		if issue.Title != "Test Issue" {
			t.Errorf("Title = %q, want %q", issue.Title, "Test Issue")
		}
		if issue.Body != "This is the body" {
			t.Errorf("Body = %q, want %q", issue.Body, "This is the body")
		}
		if issue.State != "open" {
			t.Errorf("State = %q, want %q", issue.State, "open")
		}
		if issue.Author != "octocat" {
			t.Errorf("Author = %q, want %q", issue.Author, "octocat")
		}
		if len(issue.Labels) != 2 {
			t.Fatalf("Labels length = %d, want 2", len(issue.Labels))
		}
		if issue.Labels[0] != "bug" {
			t.Errorf("Labels[0] = %q, want %q", issue.Labels[0], "bug")
		}
		if issue.Labels[1] != "help wanted" {
			t.Errorf("Labels[1] = %q, want %q", issue.Labels[1], "help wanted")
		}
		if !issue.CreatedAt.Equal(created.Time) {
			t.Errorf("CreatedAt = %v, want %v", issue.CreatedAt, created.Time)
		}
		if !issue.UpdatedAt.Equal(updated.Time) {
			t.Errorf("UpdatedAt = %v, want %v", issue.UpdatedAt, updated.Time)
		}
	})

	t.Run("handles nil user", func(t *testing.T) {
		gh := &gogithub.Issue{
			Number: intPtr(1),
			Title:  strPtr("No User"),
			Body:   strPtr(""),
			State:  strPtr("open"),
			User:   nil,
		}

		issue := convertGHIssue(gh)

		if issue.Author != "" {
			t.Errorf("Author = %q, want empty string", issue.Author)
		}
	})

	t.Run("handles nil timestamps", func(t *testing.T) {
		gh := &gogithub.Issue{
			Number:    intPtr(2),
			Title:     strPtr("No Timestamps"),
			Body:      strPtr(""),
			State:     strPtr("closed"),
			CreatedAt: nil,
			UpdatedAt: nil,
		}

		issue := convertGHIssue(gh)

		if !issue.CreatedAt.IsZero() {
			t.Errorf("CreatedAt = %v, want zero time", issue.CreatedAt)
		}
		if !issue.UpdatedAt.IsZero() {
			t.Errorf("UpdatedAt = %v, want zero time", issue.UpdatedAt)
		}
	})

	t.Run("handles empty labels", func(t *testing.T) {
		gh := &gogithub.Issue{
			Number: intPtr(3),
			Title:  strPtr("No Labels"),
			Body:   strPtr(""),
			State:  strPtr("open"),
			Labels: []*gogithub.Label{},
		}

		issue := convertGHIssue(gh)

		if len(issue.Labels) != 0 {
			t.Errorf("Labels length = %d, want 0", len(issue.Labels))
		}
	})

	t.Run("handles nil fields via getter methods", func(t *testing.T) {
		// go-github's Get* methods return zero values for nil fields
		gh := &gogithub.Issue{}

		issue := convertGHIssue(gh)

		if issue.Number != 0 {
			t.Errorf("Number = %d, want 0", issue.Number)
		}
		if issue.Title != "" {
			t.Errorf("Title = %q, want empty string", issue.Title)
		}
		if issue.Body != "" {
			t.Errorf("Body = %q, want empty string", issue.Body)
		}
		if issue.State != "" {
			t.Errorf("State = %q, want empty string", issue.State)
		}
	})
}

// Helper functions for creating pointers to primitives.
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
