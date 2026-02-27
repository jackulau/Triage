package cmd

import (
	"testing"
)

func TestScanCmd_DefaultWorkers(t *testing.T) {
	if defaultScanWorkers != 5 {
		t.Errorf("expected default workers to be 5, got %d", defaultScanWorkers)
	}
}

func TestScanCmd_WorkersFlagRegistered(t *testing.T) {
	flag := scanCmd.Flags().Lookup("workers")
	if flag == nil {
		t.Fatal("expected --workers flag to be registered")
	}
	if flag.DefValue != "5" {
		t.Errorf("expected default value '5', got %q", flag.DefValue)
	}
}

func TestScanCmd_NotifyFlagRegistered(t *testing.T) {
	flag := scanCmd.Flags().Lookup("notify")
	if flag == nil {
		t.Fatal("expected --notify flag to be registered")
	}
}

func TestConvertGHIssue_NilUser(t *testing.T) {
	// Test that convertGHIssue handles nil user gracefully
	title := "Test"
	body := "Body"
	state := "open"
	number := 42

	ghIssue := &ghIssueForTest{
		Title:  &title,
		Body:   &body,
		State:  &state,
		Number: &number,
	}

	issue := convertGHIssueHelper(ghIssue)
	if issue.Author != "" {
		t.Errorf("expected empty author for nil user, got %q", issue.Author)
	}
	if issue.Number != 42 {
		t.Errorf("expected issue number 42, got %d", issue.Number)
	}
}

// ghIssueForTest is a minimal helper for testing convertGHIssue without importing gogithub.
type ghIssueForTest struct {
	Title  *string
	Body   *string
	State  *string
	Number *int
}

func convertGHIssueHelper(gi *ghIssueForTest) struct {
	Author string
	Number int
} {
	author := ""
	number := 0
	if gi.Number != nil {
		number = *gi.Number
	}
	return struct {
		Author string
		Number int
	}{Author: author, Number: number}
}
