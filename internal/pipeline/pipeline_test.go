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

// mockStore implements PipelineStore for testing without SQLite.
type mockStore struct {
	mu         sync.Mutex
	repos      map[string]*store.Repo
	nextRepoID int64
	triageLogs []*store.TriageLog
	createErr  error
	getRepoErr error
	logErr     error
}

func newMockStore() *mockStore {
	return &mockStore{
		repos:      make(map[string]*store.Repo),
		nextRepoID: 1,
	}
}

func (m *mockStore) GetRepoByOwnerRepo(owner, repo string) (*store.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getRepoErr != nil {
		return nil, m.getRepoErr
	}
	key := owner + "/" + repo
	r, ok := m.repos[key]
	if !ok {
		return nil, errors.New("scanning repo: no rows in result set")
	}
	return r, nil
}

func (m *mockStore) CreateRepo(owner, repo string) (*store.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return nil, m.createErr
	}
	key := owner + "/" + repo
	r := &store.Repo{
		ID:       m.nextRepoID,
		Owner:    owner,
		RepoName: repo,
	}
	m.nextRepoID++
	m.repos[key] = r
	return r, nil
}

func (m *mockStore) LogTriageAction(log *store.TriageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.logErr != nil {
		return m.logErr
	}
	m.triageLogs = append(m.triageLogs, log)
	return nil
}

// mockEmbeddingStore implements dedup.EmbeddingStore for testing without SQLite.
type mockEmbeddingStore struct {
	mu         sync.Mutex
	embeddings map[int64]map[int][]byte // repoID -> number -> embedding
}

func newMockEmbeddingStore() *mockEmbeddingStore {
	return &mockEmbeddingStore{
		embeddings: make(map[int64]map[int][]byte),
	}
}

func (m *mockEmbeddingStore) GetEmbeddingsForRepo(repoID int64) ([]store.IssueEmbedding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byNumber, ok := m.embeddings[repoID]
	if !ok {
		return nil, nil
	}
	var results []store.IssueEmbedding
	for num, emb := range byNumber {
		results = append(results, store.IssueEmbedding{
			Number:    num,
			Embedding: emb,
			Model:     "test-model",
		})
	}
	return results, nil
}

func (m *mockEmbeddingStore) UpdateEmbedding(repoID int64, number int, embedding []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.embeddings[repoID] == nil {
		m.embeddings[repoID] = make(map[int][]byte)
	}
	m.embeddings[repoID][number] = embedding
	return nil
}

func testLabels() []config.LabelConfig {
	return []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature or request"},
		{Name: "question", Description: "Further information is requested"},
	}
}

func setupTestPipeline(t *testing.T) (*Pipeline, *mockStore, *pubsub.Broker[github.IssueEvent], *mockEmbedder, *mockCompleter, *mockNotifier) {
	t.Helper()

	mockSt := newMockStore()
	embStore := newMockEmbeddingStore()

	broker := pubsub.NewBroker[github.IssueEvent]()

	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "This is a bug report"}`,
	}
	notifier := &mockNotifier{}

	dedupEngine := dedup.NewEngine(embedder, embStore)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   notifier,
		Store:      mockSt,
		Broker:     broker,
		Labels:     testLabels(),
		Logger:     slog.Default(),
	})

	return p, mockSt, broker, embedder, completer, notifier
}

func TestPipelineProcessesNewIssue(t *testing.T) {
	p, mockSt, broker, _, completer, notifier := setupTestPipeline(t)

	// Create repo first
	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
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
	p, mockSt, broker, embedder, completer, notifier := setupTestPipeline(t)

	// Make embedder fail
	embedder.mu.Lock()
	embedder.err = errors.New("embedding service unavailable")
	embedder.mu.Unlock()

	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
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
	p, mockSt, broker, _, completer, notifier := setupTestPipeline(t)

	// Make completer fail
	completer.mu.Lock()
	completer.err = errors.New("LLM service unavailable")
	completer.mu.Unlock()

	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
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
	p, mockSt, broker, _, _, notifier := setupTestPipeline(t)

	// Make notifier fail
	notifier.mu.Lock()
	notifier.err = errors.New("notification service unavailable")
	notifier.mu.Unlock()

	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
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
	p, mockSt, _, _, _, _ := setupTestPipeline(t)

	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
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

func TestPipelineStoreInterface(t *testing.T) {
	// Verify that *store.DB satisfies PipelineStore interface at compile time.
	var _ PipelineStore = (*store.DB)(nil)
}

func TestPipelineMockStoreLogsTriageAction(t *testing.T) {
	p, mockSt, _, _, _, _ := setupTestPipeline(t)

	_, err := mockSt.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	ctx := context.Background()
	_, err = p.ProcessSingleIssue(ctx, "owner/repo", github.Issue{
		Number: 5,
		Title:  "Triage me",
		Body:   "Please triage",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mockSt.mu.Lock()
	defer mockSt.mu.Unlock()

	if len(mockSt.triageLogs) == 0 {
		t.Fatal("expected at least one triage log entry")
	}
	if mockSt.triageLogs[0].IssueNumber != 5 {
		t.Errorf("expected issue number 5, got %d", mockSt.triageLogs[0].IssueNumber)
	}
}
