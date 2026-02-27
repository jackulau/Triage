package dedup

import (
	"context"
	"testing"
	"time"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/store"
)

func TestContentHash_Deterministic(t *testing.T) {
	hash1 := ContentHash("title", "body")
	hash2 := ContentHash("title", "body")
	if hash1 != hash2 {
		t.Errorf("expected same hash for same input, got %s vs %s", hash1, hash2)
	}
}

func TestContentHash_DifferentForDifferentContent(t *testing.T) {
	hash1 := ContentHash("title", "body")
	hash2 := ContentHash("title", "different body")
	if hash1 == hash2 {
		t.Error("expected different hashes for different input")
	}
}

func TestContentHash_DifferentForDifferentTitle(t *testing.T) {
	hash1 := ContentHash("title one", "body")
	hash2 := ContentHash("title two", "body")
	if hash1 == hash2 {
		t.Error("expected different hashes for different titles")
	}
}

func TestContentHash_EmptyBody(t *testing.T) {
	hash := ContentHash("title", "")
	if hash == "" {
		t.Error("expected non-empty hash for empty body")
	}
}

func TestEngine_SkipsReembeddingUnchangedIssue(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	vec := []float32{0.5, 0.5, 0.0}
	embedder.addEmbedding("Cached issue\n\nCached body", vec)

	// Upsert issue
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    1,
		Title:     "Cached issue",
		Body:      "Cached body",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.85))

	// First call: should embed (no cache yet)
	_, err = engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Cached issue",
		Body:   "Cached body",
	})
	if err != nil {
		t.Fatalf("first CheckDuplicate error: %v", err)
	}

	firstCallCount := embedder.callCount
	if firstCallCount != 1 {
		t.Fatalf("expected 1 embed call on first run, got %d", firstCallCount)
	}

	// Second call with same content: should skip re-embedding
	_, err = engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Cached issue",
		Body:   "Cached body",
	})
	if err != nil {
		t.Fatalf("second CheckDuplicate error: %v", err)
	}

	if embedder.callCount != firstCallCount {
		t.Errorf("expected embed call count to remain %d (cached), got %d", firstCallCount, embedder.callCount)
	}
}

func TestEngine_ReembedsChangedIssue(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	vec := []float32{0.5, 0.5, 0.0}
	embedder.addEmbedding("Issue title\n\nOriginal body", vec)
	embedder.addEmbedding("Issue title\n\nUpdated body", []float32{0.6, 0.4, 0.0})

	// Upsert issue with original content
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    1,
		Title:     "Issue title",
		Body:      "Original body",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.85))

	// First call: embed original content
	_, err = engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Issue title",
		Body:   "Original body",
	})
	if err != nil {
		t.Fatalf("first CheckDuplicate error: %v", err)
	}

	firstCallCount := embedder.callCount

	// Second call with different body: should re-embed
	_, err = engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Issue title",
		Body:   "Updated body",
	})
	if err != nil {
		t.Fatalf("second CheckDuplicate error: %v", err)
	}

	if embedder.callCount == firstCallCount {
		t.Error("expected re-embedding when content changed, but embed was not called")
	}
}

func TestEngine_ComposeTextExported(t *testing.T) {
	engine := NewEngine(nil, nil, WithMaxChars(100))

	issue := github.Issue{
		Title: "Test title",
		Body:  "Test body",
	}

	result := engine.ComposeText(issue)
	if result != "Test title\n\nTest body" {
		t.Errorf("expected 'Test title\\n\\nTest body', got %q", result)
	}
}

func TestStore_UpdateEmbeddingWithHash(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Test",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	embedding := EncodeEmbedding([]float32{0.1, 0.2, 0.3})
	hash := ContentHash("Test", "")

	err = db.UpdateEmbeddingWithHash(repo.ID, 1, embedding, "model", hash)
	if err != nil {
		t.Fatalf("updating embedding with hash: %v", err)
	}

	// Verify hash was stored
	storedHash, hasEmb, err := db.GetIssueEmbeddingHash(repo.ID, 1)
	if err != nil {
		t.Fatalf("getting embedding hash: %v", err)
	}
	if !hasEmb {
		t.Error("expected issue to have embedding")
	}
	if storedHash != hash {
		t.Errorf("expected hash %q, got %q", hash, storedHash)
	}
}

func TestStore_GetIssueEmbeddingHash_NoIssue(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	hash, hasEmb, err := db.GetIssueEmbeddingHash(repo.ID, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasEmb {
		t.Error("expected no embedding for non-existent issue")
	}
	if hash != "" {
		t.Errorf("expected empty hash, got %q", hash)
	}
}

func TestStore_GetIssueEmbeddingHash_NoEmbedding(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer db.Close()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	// Insert issue without embedding
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Test",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	_, hasEmb, err := db.GetIssueEmbeddingHash(repo.ID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasEmb {
		t.Error("expected no embedding for issue without embedding")
	}
}
