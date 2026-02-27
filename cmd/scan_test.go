package cmd

import (
	"testing"
	"time"

	gogithub "github.com/google/go-github/v60/github"
)

func TestScanCmdArgsValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no arguments",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "too many arguments",
			args:    []string{"owner/repo", "extra"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use cobra's Args validator directly
			err := scanCmd.Args(scanCmd, tt.args)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestScanCmdExactArgs(t *testing.T) {
	// Verify scan command requires exactly 1 argument
	err := scanCmd.Args(scanCmd, []string{"owner/repo"})
	if err != nil {
		t.Errorf("expected no error with exactly 1 argument, got: %v", err)
	}
}

func TestScanRepoFormatValidation(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{
			name:    "valid owner/repo",
			repo:    "owner/repo",
			wantErr: false,
		},
		{
			name:    "missing slash",
			repo:    "ownerrepo",
			wantErr: true,
		},
		{
			name:    "only owner with slash",
			repo:    "owner/",
			wantErr: false, // SplitN will produce ["owner", ""], which is 2 parts
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the repo format validation logic used in runScan
			parts := splitRepo(tt.repo)
			if tt.wantErr && parts != nil {
				t.Error("expected invalid format, got valid")
			}
			if !tt.wantErr && parts == nil {
				t.Error("expected valid format, got invalid")
			}
		})
	}
}

// splitRepo mimics the validation in runScan/runWatch.
func splitRepo(repoArg string) []string {
	parts := make([]string, 0, 2)
	slashIdx := -1
	for i, c := range repoArg {
		if c == '/' {
			slashIdx = i
			break
		}
	}
	if slashIdx == -1 {
		return nil
	}
	parts = append(parts, repoArg[:slashIdx], repoArg[slashIdx+1:])
	return parts
}

func TestConvertGHIssue(t *testing.T) {
	title := "Test title"
	body := "Test body"
	state := "open"
	login := "testuser"
	number := 42
	label1Name := "bug"
	label2Name := "critical"
	now := time.Now()
	ghTime := gogithub.Timestamp{Time: now}

	ghIssue := &gogithub.Issue{
		Number: &number,
		Title:  &title,
		Body:   &body,
		State:  &state,
		User: &gogithub.User{
			Login: &login,
		},
		Labels: []*gogithub.Label{
			{Name: &label1Name},
			{Name: &label2Name},
		},
		CreatedAt: &ghTime,
		UpdatedAt: &ghTime,
	}

	issue := convertGHIssue(ghIssue)

	if issue.Number != number {
		t.Errorf("Number: expected %d, got %d", number, issue.Number)
	}
	if issue.Title != title {
		t.Errorf("Title: expected %q, got %q", title, issue.Title)
	}
	if issue.Body != body {
		t.Errorf("Body: expected %q, got %q", body, issue.Body)
	}
	if issue.State != state {
		t.Errorf("State: expected %q, got %q", state, issue.State)
	}
	if issue.Author != login {
		t.Errorf("Author: expected %q, got %q", login, issue.Author)
	}
	if len(issue.Labels) != 2 {
		t.Fatalf("Labels: expected 2, got %d", len(issue.Labels))
	}
	if issue.Labels[0] != label1Name {
		t.Errorf("Labels[0]: expected %q, got %q", label1Name, issue.Labels[0])
	}
	if issue.Labels[1] != label2Name {
		t.Errorf("Labels[1]: expected %q, got %q", label2Name, issue.Labels[1])
	}
	if !issue.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: expected %v, got %v", now, issue.CreatedAt)
	}
	if !issue.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt: expected %v, got %v", now, issue.UpdatedAt)
	}
}

func TestConvertGHIssueMinimalFields(t *testing.T) {
	// Test with nil optional fields
	number := 1
	title := "Minimal"
	state := "open"

	ghIssue := &gogithub.Issue{
		Number: &number,
		Title:  &title,
		State:  &state,
		// No User, no Labels, no Body, no timestamps
	}

	issue := convertGHIssue(ghIssue)

	if issue.Number != number {
		t.Errorf("Number: expected %d, got %d", number, issue.Number)
	}
	if issue.Title != title {
		t.Errorf("Title: expected %q, got %q", title, issue.Title)
	}
	if issue.Author != "" {
		t.Errorf("Author: expected empty, got %q", issue.Author)
	}
	if len(issue.Labels) != 0 {
		t.Errorf("Labels: expected empty, got %v", issue.Labels)
	}
}

func TestConvertGHIssuePtrMinimal(t *testing.T) {
	// Test convertGHIssuePtr (from check.go) with minimal fields
	number := 5
	title := "Ptr minimal"
	state := "closed"

	ghIssue := &gogithub.Issue{
		Number: &number,
		Title:  &title,
		State:  &state,
	}

	issue := convertGHIssuePtr(ghIssue)

	if issue.Number != number {
		t.Errorf("Number: expected %d, got %d", number, issue.Number)
	}
	if issue.State != state {
		t.Errorf("State: expected %q, got %q", state, issue.State)
	}
	if issue.Author != "" {
		t.Errorf("Author: expected empty, got %q", issue.Author)
	}
}

func TestScanCmdHasNotifyFlag(t *testing.T) {
	flag := scanCmd.Flags().Lookup("notify")
	if flag == nil {
		t.Fatal("expected scan command to have --notify flag")
	}
	if flag.DefValue != "" {
		t.Errorf("expected --notify default to be empty, got %q", flag.DefValue)
	}
}
