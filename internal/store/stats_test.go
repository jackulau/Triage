package store

import (
	"testing"
	"time"
)

func TestGetRepoStats_Empty(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	stats, err := db.GetRepoStats(repo.ID)
	if err != nil {
		t.Fatalf("getting stats: %v", err)
	}

	if stats.IssueCount != 0 {
		t.Errorf("expected 0 issues, got %d", stats.IssueCount)
	}
	if stats.EmbeddingCount != 0 {
		t.Errorf("expected 0 embeddings, got %d", stats.EmbeddingCount)
	}
	if stats.ClassifiedCount != 0 {
		t.Errorf("expected 0 classified, got %d", stats.ClassifiedCount)
	}
	if stats.Repo.Owner != "owner" {
		t.Errorf("expected owner 'owner', got %q", stats.Repo.Owner)
	}
	if stats.Repo.RepoName != "repo" {
		t.Errorf("expected repo 'repo', got %q", stats.Repo.RepoName)
	}
}

func TestGetRepoStats_WithData(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("org", "myrepo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	now := time.Now()

	// Insert 3 issues
	for i := 1; i <= 3; i++ {
		err := db.UpsertIssue(&Issue{
			RepoID:    repo.ID,
			Number:    i,
			Title:     "Test issue",
			Body:      "Test body",
			State:     "open",
			Author:    "test",
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("upserting issue %d: %v", i, err)
		}
	}

	// Add embeddings to 2 of them
	for i := 1; i <= 2; i++ {
		err := db.UpdateEmbedding(repo.ID, i, []byte{0x01, 0x02}, "test-model")
		if err != nil {
			t.Fatalf("updating embedding %d: %v", i, err)
		}
	}

	// Add triage log entries for 1 issue
	err = db.LogTriageAction(&TriageLog{
		RepoID:      repo.ID,
		IssueNumber: 1,
		Action:      "triaged",
	})
	if err != nil {
		t.Fatalf("logging triage action: %v", err)
	}

	stats, err := db.GetRepoStats(repo.ID)
	if err != nil {
		t.Fatalf("getting stats: %v", err)
	}

	if stats.IssueCount != 3 {
		t.Errorf("expected 3 issues, got %d", stats.IssueCount)
	}
	if stats.EmbeddingCount != 2 {
		t.Errorf("expected 2 embeddings, got %d", stats.EmbeddingCount)
	}
	if stats.ClassifiedCount != 1 {
		t.Errorf("expected 1 classified, got %d", stats.ClassifiedCount)
	}
}

func TestGetRepoStats_MultipleTriageLogsForSameIssue(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("org", "myrepo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	now := time.Now()
	err = db.UpsertIssue(&Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Test issue",
		State:     "open",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	// Log triage action twice for same issue
	for i := 0; i < 2; i++ {
		err = db.LogTriageAction(&TriageLog{
			RepoID:      repo.ID,
			IssueNumber: 1,
			Action:      "triaged",
		})
		if err != nil {
			t.Fatalf("logging triage action: %v", err)
		}
	}

	stats, err := db.GetRepoStats(repo.ID)
	if err != nil {
		t.Fatalf("getting stats: %v", err)
	}

	// Should count distinct issue numbers, not log entries
	if stats.ClassifiedCount != 1 {
		t.Errorf("expected 1 classified (distinct), got %d", stats.ClassifiedCount)
	}
}

func TestGetAllRepoStats(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	// Empty db
	stats, err := db.GetAllRepoStats()
	if err != nil {
		t.Fatalf("getting all stats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 stats, got %d", len(stats))
	}

	// Add two repos
	repo1, err := db.CreateRepo("org", "repo1")
	if err != nil {
		t.Fatalf("creating repo1: %v", err)
	}
	repo2, err := db.CreateRepo("org", "repo2")
	if err != nil {
		t.Fatalf("creating repo2: %v", err)
	}

	now := time.Now()

	// Add 2 issues to repo1
	for i := 1; i <= 2; i++ {
		_ = db.UpsertIssue(&Issue{
			RepoID: repo1.ID, Number: i, Title: "Issue", State: "open",
			CreatedAt: now, UpdatedAt: now,
		})
	}

	// Add 1 issue to repo2
	_ = db.UpsertIssue(&Issue{
		RepoID: repo2.ID, Number: 1, Title: "Issue", State: "open",
		CreatedAt: now, UpdatedAt: now,
	})

	stats, err = db.GetAllRepoStats()
	if err != nil {
		t.Fatalf("getting all stats: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(stats))
	}

	if stats[0].IssueCount != 2 {
		t.Errorf("repo1: expected 2 issues, got %d", stats[0].IssueCount)
	}
	if stats[1].IssueCount != 1 {
		t.Errorf("repo2: expected 1 issue, got %d", stats[1].IssueCount)
	}
}

func TestGetRepoStats_InvalidRepo(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	_, err = db.GetRepoStats(9999)
	if err == nil {
		t.Error("expected error for non-existent repo")
	}
}
