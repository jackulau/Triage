package store

import (
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigration(t *testing.T) {
	db := setupTestDB(t)

	var version int
	err := db.Conn().QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("expected user_version 1, got %d", version)
	}
}

func TestReposCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create
	repo, err := db.CreateRepo("octocat", "hello-world")
	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}
	if repo.Owner != "octocat" || repo.RepoName != "hello-world" {
		t.Errorf("unexpected repo: %+v", repo)
	}
	if repo.ID == 0 {
		t.Error("expected non-zero repo ID")
	}

	// Get by ID
	got, err := db.GetRepo(repo.ID)
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}
	if got.Owner != "octocat" {
		t.Errorf("expected owner 'octocat', got %q", got.Owner)
	}

	// Get by owner/repo
	got2, err := db.GetRepoByOwnerRepo("octocat", "hello-world")
	if err != nil {
		t.Fatalf("GetRepoByOwnerRepo failed: %v", err)
	}
	if got2.ID != repo.ID {
		t.Errorf("expected same ID, got %d vs %d", got2.ID, repo.ID)
	}

	// Update poll state
	now := time.Now().UTC()
	err = db.UpdatePollState(repo.ID, now, "etag-123")
	if err != nil {
		t.Fatalf("UpdatePollState failed: %v", err)
	}

	updated, _ := db.GetRepo(repo.ID)
	if updated.LastPolledAt == nil {
		t.Error("expected non-nil LastPolledAt")
	}
	if updated.ETag != "etag-123" {
		t.Errorf("expected etag 'etag-123', got %q", updated.ETag)
	}

	// List
	_, err = db.CreateRepo("other", "repo")
	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	repos, err := db.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(repos))
	}
}

func TestRepoDuplicate(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.CreateRepo("octocat", "hello-world")
	if err != nil {
		t.Fatalf("first CreateRepo failed: %v", err)
	}

	_, err = db.CreateRepo("octocat", "hello-world")
	if err == nil {
		t.Error("expected error on duplicate repo, got nil")
	}
}

func TestIssuesCRUD(t *testing.T) {
	db := setupTestDB(t)

	repo, _ := db.CreateRepo("octocat", "hello-world")

	now := time.Now().UTC()
	issue := &Issue{
		RepoID:    repo.ID,
		Number:    42,
		Title:     "Test issue",
		Body:      "This is a test",
		BodyHash:  "abc123",
		State:     "open",
		Author:    "testuser",
		Labels:    []string{"bug", "help wanted"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Upsert (insert)
	err := db.UpsertIssue(issue)
	if err != nil {
		t.Fatalf("UpsertIssue failed: %v", err)
	}

	// Get
	got, err := db.GetIssue(repo.ID, 42)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Title != "Test issue" {
		t.Errorf("expected title 'Test issue', got %q", got.Title)
	}
	if got.Author != "testuser" {
		t.Errorf("expected author 'testuser', got %q", got.Author)
	}
	if len(got.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(got.Labels))
	}
	if got.Labels[0] != "bug" {
		t.Errorf("expected first label 'bug', got %q", got.Labels[0])
	}

	// Upsert (update)
	issue.Title = "Updated title"
	issue.State = "closed"
	err = db.UpsertIssue(issue)
	if err != nil {
		t.Fatalf("UpsertIssue (update) failed: %v", err)
	}

	got2, _ := db.GetIssue(repo.ID, 42)
	if got2.Title != "Updated title" {
		t.Errorf("expected updated title, got %q", got2.Title)
	}
	if got2.State != "closed" {
		t.Errorf("expected state 'closed', got %q", got2.State)
	}

	// GetIssuesByRepo
	issue2 := &Issue{
		RepoID:    repo.ID,
		Number:    43,
		Title:     "Another issue",
		State:     "open",
		CreatedAt: now,
		UpdatedAt: now,
	}
	db.UpsertIssue(issue2)

	issues, err := db.GetIssuesByRepo(repo.ID)
	if err != nil {
		t.Fatalf("GetIssuesByRepo failed: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(issues))
	}
}

func TestUpdateEmbedding(t *testing.T) {
	db := setupTestDB(t)

	repo, _ := db.CreateRepo("octocat", "hello-world")
	now := time.Now().UTC()

	issue := &Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Embedding test",
		State:     "open",
		CreatedAt: now,
		UpdatedAt: now,
	}
	db.UpsertIssue(issue)

	embedding := []byte{0x01, 0x02, 0x03, 0x04}
	err := db.UpdateEmbedding(repo.ID, 1, embedding, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("UpdateEmbedding failed: %v", err)
	}

	got, _ := db.GetIssue(repo.ID, 1)
	if got.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("expected model 'text-embedding-3-small', got %q", got.EmbeddingModel)
	}
	if got.EmbeddedAt == nil {
		t.Error("expected non-nil EmbeddedAt")
	}
	if len(got.Embedding) != 4 {
		t.Errorf("expected embedding length 4, got %d", len(got.Embedding))
	}

	// GetEmbeddingsForRepo
	embeddings, err := db.GetEmbeddingsForRepo(repo.ID)
	if err != nil {
		t.Fatalf("GetEmbeddingsForRepo failed: %v", err)
	}
	if len(embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(embeddings))
	}
	if embeddings[0].Number != 1 {
		t.Errorf("expected issue number 1, got %d", embeddings[0].Number)
	}
}

func TestTriageLog(t *testing.T) {
	db := setupTestDB(t)

	repo, _ := db.CreateRepo("octocat", "hello-world")

	log := &TriageLog{
		RepoID:          repo.ID,
		IssueNumber:     42,
		Action:          "classified",
		SuggestedLabels: "bug,enhancement",
		Reasoning:       "Looks like a bug report",
		NotifiedVia:     "slack",
	}

	err := db.LogTriageAction(log)
	if err != nil {
		t.Fatalf("LogTriageAction failed: %v", err)
	}

	// Get
	logs, err := db.GetTriageLog(repo.ID, 42)
	if err != nil {
		t.Fatalf("GetTriageLog failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Action != "classified" {
		t.Errorf("expected action 'classified', got %q", logs[0].Action)
	}
	if logs[0].SuggestedLabels != "bug,enhancement" {
		t.Errorf("expected labels 'bug,enhancement', got %q", logs[0].SuggestedLabels)
	}

	// Update human decision
	err = db.UpdateHumanDecision(logs[0].ID, "approved")
	if err != nil {
		t.Fatalf("UpdateHumanDecision failed: %v", err)
	}

	updated, _ := db.GetTriageLog(repo.ID, 42)
	if updated[0].HumanDecision != "approved" {
		t.Errorf("expected decision 'approved', got %q", updated[0].HumanDecision)
	}
}

func TestTriageLogDuplicate(t *testing.T) {
	db := setupTestDB(t)

	repo, _ := db.CreateRepo("octocat", "hello-world")

	log := &TriageLog{
		RepoID:      repo.ID,
		IssueNumber: 10,
		Action:      "duplicate_detected",
		DuplicateOf: "#5",
		Reasoning:   "Very similar to issue #5",
	}

	err := db.LogTriageAction(log)
	if err != nil {
		t.Fatalf("LogTriageAction failed: %v", err)
	}

	logs, _ := db.GetTriageLog(repo.ID, 10)
	if logs[0].DuplicateOf != "#5" {
		t.Errorf("expected duplicate_of '#5', got %q", logs[0].DuplicateOf)
	}
}
