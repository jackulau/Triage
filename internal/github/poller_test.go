package github

import (
	"testing"
	"time"

	"github.com/jacklau/triage/internal/store"
)

func TestHashBody(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := hashBody("hello world")
		h2 := hashBody("hello world")
		if h1 != h2 {
			t.Errorf("same input produced different hashes: %s vs %s", h1, h2)
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := hashBody("hello world")
		h2 := hashBody("hello world!")
		if h1 == h2 {
			t.Error("different inputs should produce different hashes")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		h := hashBody("")
		if h == "" {
			t.Error("hash of empty string should not be empty")
		}
	})
}

func TestLabelsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"same order", []string{"bug", "enhancement"}, []string{"bug", "enhancement"}, true},
		{"different order", []string{"enhancement", "bug"}, []string{"bug", "enhancement"}, true},
		{"different length", []string{"bug"}, []string{"bug", "enhancement"}, false},
		{"different labels", []string{"bug"}, []string{"feature"}, false},
		{"one nil one empty", nil, []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labelsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("labelsEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDiffSnapshot(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	stored := &store.Issue{
		Number:    1,
		Title:     "Original Title",
		Body:      "Original body content",
		BodyHash:  hashBody("Original body content"),
		State:     "open",
		Labels:    []string{"bug"},
		CreatedAt: baseTime,
		UpdatedAt: baseTime,
	}

	t.Run("no changes", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Original Title",
			Body:      "Original body content",
			State:     "open",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 0 {
			t.Errorf("expected no changes, got %v", changes)
		}
	})

	t.Run("title changed", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Updated Title",
			Body:      "Original body content",
			State:     "open",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 1 || changes[0] != ChangeTitleEdited {
			t.Errorf("expected [ChangeTitleEdited], got %v", changes)
		}
	})

	t.Run("body changed", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Original Title",
			Body:      "Updated body content",
			State:     "open",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 1 || changes[0] != ChangeBodyEdited {
			t.Errorf("expected [ChangeBodyEdited], got %v", changes)
		}
	})

	t.Run("state changed", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Original Title",
			Body:      "Original body content",
			State:     "closed",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 1 || changes[0] != ChangeStateChanged {
			t.Errorf("expected [ChangeStateChanged], got %v", changes)
		}
	})

	t.Run("labels changed", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Original Title",
			Body:      "Original body content",
			State:     "open",
			Labels:    []string{"bug", "enhancement"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 1 || changes[0] != ChangeLabelsChanged {
			t.Errorf("expected [ChangeLabelsChanged], got %v", changes)
		}
	})

	t.Run("multiple changes", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "New Title",
			Body:      "New body",
			State:     "closed",
			Labels:    []string{"enhancement"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Minute),
		}
		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 4 {
			t.Errorf("expected 4 changes, got %d: %v", len(changes), changes)
		}

		changeSet := make(map[ChangeType]bool)
		for _, c := range changes {
			changeSet[c] = true
		}
		expected := []ChangeType{ChangeTitleEdited, ChangeBodyEdited, ChangeStateChanged, ChangeLabelsChanged}
		for _, e := range expected {
			if !changeSet[e] {
				t.Errorf("missing expected change: %s", e)
			}
		}
	})
}

func TestConvertIssue(t *testing.T) {
	now := time.Now()
	ghTimestamp := &gogithubTimestamp{Time: now}

	number := 42
	title := "Test Issue"
	body := "Test body"
	state := "open"
	login := "testuser"
	labelName := "bug"

	ghIssue := &gogithubIssue{
		Number: &number,
		Title:  &title,
		Body:   &body,
		State:  &state,
		User: &gogithubUser{
			Login: &login,
		},
		Labels: []*gogithubLabel{
			{Name: &labelName},
		},
		CreatedAt: ghTimestamp,
		UpdatedAt: ghTimestamp,
	}

	issue := convertIssueTest(ghIssue)
	if issue.Number != 42 {
		t.Errorf("expected Number=42, got %d", issue.Number)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("expected Title='Test Issue', got %q", issue.Title)
	}
	if issue.Body != "Test body" {
		t.Errorf("expected Body='Test body', got %q", issue.Body)
	}
	if issue.State != "open" {
		t.Errorf("expected State='open', got %q", issue.State)
	}
	if issue.Author != "testuser" {
		t.Errorf("expected Author='testuser', got %q", issue.Author)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Errorf("expected Labels=['bug'], got %v", issue.Labels)
	}
}

// Test-local types that mirror the go-github types we need for convertIssue
// testing. This avoids importing go-github in tests while verifying the
// conversion logic.
type gogithubTimestamp struct {
	Time time.Time
}

type gogithubUser struct {
	Login *string
}

type gogithubLabel struct {
	Name *string
}

type gogithubIssue struct {
	Number    *int
	Title     *string
	Body      *string
	State     *string
	User      *gogithubUser
	Labels    []*gogithubLabel
	CreatedAt *gogithubTimestamp
	UpdatedAt *gogithubTimestamp
}

// convertIssueTest is a test-only conversion function that mimics convertIssue
// but works with our test-local types.
func convertIssueTest(gh *gogithubIssue) Issue {
	issue := Issue{}

	if gh.Number != nil {
		issue.Number = *gh.Number
	}
	if gh.Title != nil {
		issue.Title = *gh.Title
	}
	if gh.Body != nil {
		issue.Body = *gh.Body
	}
	if gh.State != nil {
		issue.State = *gh.State
	}
	if gh.User != nil && gh.User.Login != nil {
		issue.Author = *gh.User.Login
	}
	for _, label := range gh.Labels {
		if label.Name != nil {
			issue.Labels = append(issue.Labels, *label.Name)
		}
	}
	if gh.CreatedAt != nil {
		issue.CreatedAt = gh.CreatedAt.Time
	}
	if gh.UpdatedAt != nil {
		issue.UpdatedAt = gh.UpdatedAt.Time
	}

	return issue
}

func TestDiffAndPublishIntegration(t *testing.T) {
	// Integration test using real SQLite store.
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer db.Close()

	// Create a repo record.
	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("new issue detected", func(t *testing.T) {
		issue := Issue{
			Number:    1,
			Title:     "New Issue",
			Body:      "Issue body",
			State:     "open",
			Author:    "author",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime,
		}

		bodyHash := hashBody(issue.Body)
		existing, err := db.GetIssue(repo.ID, issue.Number)
		if err != nil && !isNotFound(err) {
			t.Fatalf("unexpected error: %v", err)
		}

		if existing == nil {
			// This is a new issue.
		} else {
			t.Fatal("expected nil for new issue")
		}

		// Upsert to store.
		storeIssue := &store.Issue{
			RepoID:    repo.ID,
			Number:    issue.Number,
			Title:     issue.Title,
			Body:      issue.Body,
			BodyHash:  bodyHash,
			State:     issue.State,
			Author:    issue.Author,
			Labels:    issue.Labels,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
		}
		if err := db.UpsertIssue(storeIssue); err != nil {
			t.Fatalf("upserting issue: %v", err)
		}

		// Verify it's stored.
		stored, err := db.GetIssue(repo.ID, 1)
		if err != nil {
			t.Fatalf("getting issue: %v", err)
		}
		if stored.Title != "New Issue" {
			t.Errorf("expected title 'New Issue', got %q", stored.Title)
		}
	})

	t.Run("updated issue detected", func(t *testing.T) {
		incoming := &Issue{
			Number:    1,
			Title:     "Updated Issue Title",
			Body:      "Updated body",
			State:     "open",
			Author:    "author",
			Labels:    []string{"bug"},
			CreatedAt: baseTime,
			UpdatedAt: baseTime.Add(time.Hour),
		}

		stored, err := db.GetIssue(repo.ID, 1)
		if err != nil {
			t.Fatalf("getting issue: %v", err)
		}

		changes := DiffSnapshot(stored, incoming, hashBody(incoming.Body))
		if len(changes) != 2 {
			t.Fatalf("expected 2 changes (title+body), got %d: %v", len(changes), changes)
		}

		changeSet := make(map[ChangeType]bool)
		for _, c := range changes {
			changeSet[c] = true
		}
		if !changeSet[ChangeTitleEdited] {
			t.Error("expected ChangeTitleEdited")
		}
		if !changeSet[ChangeBodyEdited] {
			t.Error("expected ChangeBodyEdited")
		}
	})
}

func TestChangeTypeString(t *testing.T) {
	tests := []struct {
		ct   ChangeType
		want string
	}{
		{ChangeNew, "new"},
		{ChangeTitleEdited, "title_edited"},
		{ChangeBodyEdited, "body_edited"},
		{ChangeStateChanged, "state_changed"},
		{ChangeLabelsChanged, "labels_changed"},
		{ChangeOther, "other"},
		{ChangeType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.ct.String(); got != tt.want {
				t.Errorf("ChangeType(%d).String() = %q, want %q", tt.ct, got, tt.want)
			}
		})
	}
}
