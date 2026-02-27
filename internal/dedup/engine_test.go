package dedup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/store"
)

// mockEmbedder is a test Embedder that returns a fixed vector for any input.
type mockEmbedder struct {
	embeddings map[string][]float32
	callCount  int
}

func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{
		embeddings: make(map[string][]float32),
	}
}

func (m *mockEmbedder) addEmbedding(text string, vec []float32) {
	m.embeddings[text] = vec
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.callCount++
	if vec, ok := m.embeddings[text]; ok {
		return vec, nil
	}
	// Return a default vector if no specific one is registered
	return []float32{0.1, 0.2, 0.3}, nil
}

// mockEmbedderErr always returns an error.
type mockEmbedderErr struct{}

func (m *mockEmbedderErr) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("embedding failed")
}

// setupTestDB creates an in-memory store with a repo and returns the DB and repo ID.
func setupTestDB(t *testing.T) (*store.DB, int64) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repo, err := db.CreateRepo("test-owner", "test-repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	return db, repo.ID
}

// insertIssueWithEmbedding creates a stored issue and attaches an embedding.
func insertIssueWithEmbedding(t *testing.T, db *store.DB, repoID int64, number int, title string, embedding []float32) {
	t.Helper()
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    number,
		Title:     title,
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	encoded := EncodeEmbedding(embedding)
	if err := db.UpdateEmbedding(repoID, number, encoded, "test-model"); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}
}

func TestEngine_CheckDuplicate_NoDuplicates(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// Existing issue with orthogonal embedding
	insertIssueWithEmbedding(t, db, repoID, 1, "Existing issue", []float32{1, 0, 0})

	// New issue gets a very different embedding
	embedder.addEmbedding("New issue", []float32{0, 1, 0})

	// Need to upsert the new issue first so UpdateEmbedding can find it
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    2,
		Title:     "New issue",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting new issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.85))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 2,
		Title:  "New issue",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsDuplicate {
		t.Error("expected no duplicates")
	}
	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}
}

func TestEngine_CheckDuplicate_FindsDuplicate(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// Existing issue with similar embedding
	insertIssueWithEmbedding(t, db, repoID, 1, "Login page broken", []float32{0.9, 0.1, 0.0})

	// New issue is very similar
	embedder.addEmbedding("Login page not working", []float32{0.89, 0.12, 0.01})

	// Upsert new issue
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    2,
		Title:     "Login page not working",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.9))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 2,
		Title:  "Login page not working",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsDuplicate {
		t.Error("expected to find duplicate")
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}
	if result.Candidates[0].Number != 1 {
		t.Errorf("expected candidate #1, got #%d", result.Candidates[0].Number)
	}
}

func TestEngine_CheckDuplicate_MaxCandidates(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// Create 5 similar existing issues
	for i := 1; i <= 5; i++ {
		vec := []float32{0.9, 0.1, float32(i) * 0.001}
		insertIssueWithEmbedding(t, db, repoID, i, fmt.Sprintf("Issue %d", i), vec)
	}

	// New issue is similar to all
	embedder.addEmbedding("Similar issue\n\nSimilar body", []float32{0.9, 0.1, 0.003})

	// Upsert new issue
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    6,
		Title:     "Similar issue",
		Body:      "Similar body",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.5), WithMaxCandidates(2))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 6,
		Title:  "Similar issue",
		Body:   "Similar body",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Candidates) > 2 {
		t.Errorf("expected at most 2 candidates, got %d", len(result.Candidates))
	}
}

func TestEngine_CheckDuplicate_SortedByScore(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// Create issues with varying similarity
	insertIssueWithEmbedding(t, db, repoID, 1, "Low similarity", []float32{0.5, 0.5, 0.0})
	insertIssueWithEmbedding(t, db, repoID, 2, "High similarity", []float32{0.9, 0.1, 0.0})
	insertIssueWithEmbedding(t, db, repoID, 3, "Medium similarity", []float32{0.7, 0.3, 0.0})

	// New issue vector
	embedder.addEmbedding("Test issue", []float32{0.9, 0.1, 0.0})

	// Upsert new issue
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    4,
		Title:     "Test issue",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.5), WithMaxCandidates(10))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 4,
		Title:  "Test issue",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify sorted descending
	for i := 1; i < len(result.Candidates); i++ {
		if result.Candidates[i].Score > result.Candidates[i-1].Score {
			t.Errorf("candidates not sorted by descending score: %f > %f",
				result.Candidates[i].Score, result.Candidates[i-1].Score)
		}
	}

	// Issue #2 should be the top candidate (identical vector)
	if len(result.Candidates) > 0 && result.Candidates[0].Number != 2 {
		t.Errorf("expected top candidate to be #2, got #%d", result.Candidates[0].Number)
	}
}

func TestEngine_CheckDuplicate_EmbedderError(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := &mockEmbedderErr{}

	engine := NewEngine(embedder, db)

	_, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Test",
	})
	if err == nil {
		t.Fatal("expected error from failing embedder")
	}
}

func TestEngine_ComposeText(t *testing.T) {
	engine := NewEngine(nil, nil, WithMaxChars(50))

	tests := []struct {
		name     string
		issue    github.Issue
		expected string
	}{
		{
			name:     "title only",
			issue:    github.Issue{Title: "Bug report"},
			expected: "Bug report",
		},
		{
			name:     "title and body",
			issue:    github.Issue{Title: "Bug", Body: "Details here"},
			expected: "Bug\n\nDetails here",
		},
		{
			name:     "truncated body",
			issue:    github.Issue{Title: "Bug", Body: "This is a very long body that should get truncated to fit within the max chars limit"},
			expected: "Bug\n\nThis is a very long body that should get trun",
		},
		{
			name:     "empty body",
			issue:    github.Issue{Title: "Just a title", Body: ""},
			expected: "Just a title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.composeText(tt.issue)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEngine_SkipsSelfComparison(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// The issue's own embedding is identical (should be skipped)
	vec := []float32{1.0, 0.0, 0.0}
	insertIssueWithEmbedding(t, db, repoID, 1, "Issue 1", vec)

	embedder.addEmbedding("Issue 1", vec)

	engine := NewEngine(embedder, db, WithThreshold(0.5))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 1,
		Title:  "Issue 1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsDuplicate {
		t.Error("should not find self as duplicate")
	}
}

func TestEngine_IncludesClosedIssues(t *testing.T) {
	db, repoID := setupTestDB(t)
	embedder := newMockEmbedder()

	// Insert a closed issue with similar embedding
	err := db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    1,
		Title:     "Closed issue",
		State:     "closed",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting closed issue: %v", err)
	}
	encoded := EncodeEmbedding([]float32{0.9, 0.1, 0.0})
	if err := db.UpdateEmbedding(repoID, 1, encoded, "test-model"); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	// New issue is very similar
	embedder.addEmbedding("New issue", []float32{0.9, 0.1, 0.0})

	err = db.UpsertIssue(&store.Issue{
		RepoID:    repoID,
		Number:    2,
		Title:     "New issue",
		State:     "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting new issue: %v", err)
	}

	engine := NewEngine(embedder, db, WithThreshold(0.5))

	result, err := engine.CheckDuplicate(context.Background(), repoID, github.Issue{
		Number: 2,
		Title:  "New issue",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsDuplicate {
		t.Error("expected to find closed issue as duplicate")
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
	if result.Candidates[0].Number != 1 {
		t.Errorf("expected candidate #1 (closed issue), got #%d", result.Candidates[0].Number)
	}
}
