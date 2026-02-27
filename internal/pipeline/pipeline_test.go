package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jacklau/triage/internal/classify"
	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/dedup"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/store"
)

// mockEmbedder implements provider.Embedder for testing.
type mockEmbedder struct {
	mu         sync.Mutex
	embeddings map[string][]float32
	err        error
	callCount  int
}

func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{
		embeddings: make(map[string][]float32),
	}
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}
	// Return a default embedding
	return []float32{0.1, 0.2, 0.3, 0.4}, nil
}

// mockCompleter implements provider.Completer for testing.
type mockCompleter struct {
	mu        sync.Mutex
	response  string
	err       error
	callCount int
}

func (m *mockCompleter) Complete(_ context.Context, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// mockNotifier implements notify.Notifier for testing.
type mockNotifier struct {
	mu        sync.Mutex
	results   []github.TriageResult
	err       error
	callCount int
}

func (m *mockNotifier) Notify(_ context.Context, result github.TriageResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return m.err
	}
	m.results = append(m.results, result)
	return nil
}

func testLabels() []config.LabelConfig {
	return []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature or request"},
		{Name: "question", Description: "Further information is requested"},
	}
}

func setupTestPipeline(t *testing.T) (*Pipeline, *store.DB, *pubsub.Broker[github.IssueEvent], *mockEmbedder, *mockCompleter, *mockNotifier) {
	t.Helper()

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()

	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "This is a bug report"}`,
	}
	notifier := &mockNotifier{}

	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   notifier,
		Store:      db,
		Broker:     broker,
		Labels:     testLabels(),
		Logger:     slog.Default(),
	})

	return p, db, broker, embedder, completer, notifier
}

func TestPipelineProcessesNewIssue(t *testing.T) {
	p, db, broker, _, completer, notifier := setupTestPipeline(t)

	// Create repo first
	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	// Insert an issue in the store (required for dedup to embed)
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Test issue",
		Body:      "Test body",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start pipeline in background
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	// Wait for subscription to be active
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 1,
			Title:  "Test issue",
			Body:   "Test body",
			State:  "open",
			Author: "test",
		},
		ChangeType: github.ChangeNew,
	})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	completer.mu.Lock()
	defer completer.mu.Unlock()

	if notifier.callCount == 0 {
		t.Error("expected notifier to be called")
	}
	if completer.callCount == 0 {
		t.Error("expected completer to be called")
	}

	if len(notifier.results) == 0 {
		t.Fatal("expected at least one notification result")
	}

	result := notifier.results[0]
	if result.Repo != "owner/repo" {
		t.Errorf("expected repo owner/repo, got %s", result.Repo)
	}
	if result.IssueNumber != 1 {
		t.Errorf("expected issue number 1, got %d", result.IssueNumber)
	}
}

func TestPipelineIgnoresNonActionableEvents(t *testing.T) {
	p, _, broker, _, _, notifier := setupTestPipeline(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish non-actionable event
	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 1,
			Title:  "Test",
			State:  "open",
		},
		ChangeType: github.ChangeStateChanged,
	})

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	notifier.mu.Lock()
	defer notifier.mu.Unlock()

	if notifier.callCount != 0 {
		t.Errorf("expected notifier not to be called for state change, got %d calls", notifier.callCount)
	}
}

func TestPipelineHandlesEmbedderFailure(t *testing.T) {
	p, db, broker, embedder, completer, notifier := setupTestPipeline(t)

	// Make embedder fail
	embedder.mu.Lock()
	embedder.err = errors.New("embedding service unavailable")
	embedder.mu.Unlock()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    2,
		Title:     "Another issue",
		Body:      "Body text",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 2,
			Title:  "Another issue",
			Body:   "Body text",
			State:  "open",
			Author: "test",
		},
		ChangeType: github.ChangeNew,
	})

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Classifier should still be called even though embedder failed
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if completer.callCount == 0 {
		t.Error("expected classifier to still run after embedder failure")
	}

	// Notifier should still be called
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.callCount == 0 {
		t.Error("expected notifier to be called despite embedder failure")
	}
}

func TestPipelineHandlesLLMFailure(t *testing.T) {
	p, db, broker, _, completer, notifier := setupTestPipeline(t)

	// Make completer fail
	completer.mu.Lock()
	completer.err = errors.New("LLM service unavailable")
	completer.mu.Unlock()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    3,
		Title:     "Issue three",
		Body:      "Body three",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 3,
			Title:  "Issue three",
			Body:   "Body three",
			State:  "open",
			Author: "test",
		},
		ChangeType: github.ChangeNew,
	})

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Notification should still be sent with dedup results only
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.callCount == 0 {
		t.Error("expected notifier to be called with dedup results despite LLM failure")
	}
}

func TestPipelineHandlesNotificationFailure(t *testing.T) {
	p, db, broker, _, _, notifier := setupTestPipeline(t)

	// Make notifier fail
	notifier.mu.Lock()
	notifier.err = errors.New("notification service unavailable")
	notifier.mu.Unlock()

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    4,
		Title:     "Issue four",
		Body:      "Body four",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 4,
			Title:  "Issue four",
			Body:   "Body four",
			State:  "open",
			Author: "test",
		},
		ChangeType: github.ChangeNew,
	})

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Notifier should have been called twice (initial + retry)
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.callCount != 2 {
		t.Errorf("expected 2 notification calls (initial + retry), got %d", notifier.callCount)
	}
}

func TestPipelineProcessSingleIssue(t *testing.T) {
	p, db, _, _, _, _ := setupTestPipeline(t)

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	// Insert the issue so dedup can find/embed it
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    10,
		Title:     "Check this issue",
		Body:      "Check body",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx := context.Background()
	result, err := p.ProcessSingleIssue(ctx, "owner/repo", github.Issue{
		Number: 10,
		Title:  "Check this issue",
		Body:   "Check body",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Repo != "owner/repo" {
		t.Errorf("expected repo owner/repo, got %s", result.Repo)
	}
	if result.IssueNumber != 10 {
		t.Errorf("expected issue 10, got %d", result.IssueNumber)
	}
}
