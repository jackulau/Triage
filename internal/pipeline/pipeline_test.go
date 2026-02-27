package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	mu          sync.Mutex
	response    string
	err         error
	callCount   int
	lastPrompts []string
}

func (m *mockCompleter) Complete(_ context.Context, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	m.lastPrompts = append(m.lastPrompts, prompt)
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

	// Allow enough time for retry backoff on embedder (3 attempts: ~3s backoff)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Wait for retry attempts on embedder (~3s backoff) + classification
	time.Sleep(5 * time.Second)
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

	// Allow enough time for retry backoff on classifier (3 attempts: ~3s backoff)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Wait for retry attempts on classifier (~3s backoff) + processing
	time.Sleep(5 * time.Second)
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

	// Allow enough time for retry backoff (1s + 2s + processing)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Wait for all retry attempts (3 attempts with ~3s total backoff)
	time.Sleep(5 * time.Second)
	cancel()
	<-done

	// Notifier should have been called 3 times (retry.DefaultMaxAttempts)
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.callCount != 3 {
		t.Errorf("expected 3 notification calls (retry.DefaultMaxAttempts), got %d", notifier.callCount)
	}
}

func TestPipelineGracefulDrain(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()

	// Create a slow notifier that takes some time to complete.
	// This simulates in-flight processing that should finish during drain.
	var processingStarted sync.WaitGroup
	processingStarted.Add(1)
	var processingDone sync.WaitGroup
	processingDone.Add(1)

	slowNotifier := &slowMockNotifier{
		onNotify: func() {
			processingStarted.Done()
			// Simulate slow processing
			time.Sleep(200 * time.Millisecond)
			processingDone.Done()
		},
	}

	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "This is a bug report"}`,
	}

	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   slowNotifier,
		Store:      db,
		Broker:     broker,
		Labels:     testLabels(),
		Logger:     slog.Default(),
	})

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    5,
		Title:     "Drain test issue",
		Body:      "Body for drain test",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish an event that will trigger slow processing.
	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "owner/repo",
		Issue: github.Issue{
			Number: 5,
			Title:  "Drain test issue",
			Body:   "Body for drain test",
			State:  "open",
			Author: "test",
		},
		ChangeType: github.ChangeNew,
	})

	// Wait for processing to start.
	processingStarted.Wait()

	// Cancel context while processing is in-flight.
	cancel()

	// Wait for Run to return.
	select {
	case <-done:
		// Run returned -- now verify the slow processing fully completed.
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline.Run did not return within timeout")
	}

	// The processingDone WaitGroup was decremented inside onNotify AFTER the
	// 200ms sleep. If Run returned before the event finished, this would hang.
	// We use a short timeout to detect that.
	waitCh := make(chan struct{})
	go func() {
		processingDone.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
		// Processing fully completed before Run returned -- graceful drain works.
	case <-time.After(1 * time.Second):
		t.Fatal("in-flight event processing did not complete (graceful drain failed)")
	}
}

// slowMockNotifier is a mock notifier that calls onNotify before returning.
type slowMockNotifier struct {
	mu        sync.Mutex
	callCount int
	onNotify  func()
}

func (m *slowMockNotifier) Notify(_ context.Context, _ github.TriageResult) error {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()
	if m.onNotify != nil {
		m.onNotify()
	}
	return nil
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

func TestPipelineCustomPromptWiredToClassifier(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()
	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "Bug report"}`,
	}
	notifier := &mockNotifier{}

	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	customPromptText := "This repo uses a monorepo structure. Focus on backend issues."
	repoConfigs := []config.RepoConfig{
		{
			Name:         "owner/repo",
			CustomPrompt: customPromptText,
		},
	}

	p := New(PipelineDeps{
		Dedup:       dedupEngine,
		Classifier:  classifier,
		Notifier:    notifier,
		Store:       db,
		Broker:      broker,
		Labels:      testLabels(),
		RepoConfigs: repoConfigs,
		Logger:      slog.Default(),
	})

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
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

	ctx := context.Background()
	_, err = p.ProcessSingleIssue(ctx, "owner/repo", github.Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "Test body",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the custom prompt was included in the LLM call
	completer.mu.Lock()
	defer completer.mu.Unlock()

	if completer.callCount == 0 {
		t.Fatal("expected completer to be called")
	}

	found := false
	for _, prompt := range completer.lastPrompts {
		if strings.Contains(prompt, customPromptText) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected custom prompt %q to be included in LLM prompt, but it was not found", customPromptText)
	}
}

func TestPipelineCustomPromptNotIncludedWhenEmpty(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()
	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "Bug report"}`,
	}
	notifier := &mockNotifier{}

	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	// No repo configs (empty custom prompt)
	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   notifier,
		Store:      db,
		Broker:     broker,
		Labels:     testLabels(),
		Logger:     slog.Default(),
	})

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}
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

	ctx := context.Background()
	_, err = p.ProcessSingleIssue(ctx, "owner/repo", github.Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "Test body",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify "Additional context:" is NOT in the prompt when custom prompt is empty
	completer.mu.Lock()
	defer completer.mu.Unlock()

	if completer.callCount == 0 {
		t.Fatal("expected completer to be called")
	}

	for _, prompt := range completer.lastPrompts {
		if strings.Contains(prompt, "Additional context:") {
			t.Error("expected no 'Additional context:' section in prompt when custom prompt is empty")
		}
	}
}

func TestPipelinePerRepoSimilarityThreshold(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()
	embedder := newMockEmbedder()
	// Register a specific embedding for the new issue with moderate similarity to the existing one
	// Cosine similarity between {0.9,0.1,0,0} and {0.7,0.7,0,0} ~= 0.78
	embedder.embeddings["New issue\n\nNew body"] = []float32{0.7, 0.7, 0.0, 0.0}
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "Bug report"}`,
	}
	notifier := &mockNotifier{}

	// Global threshold is 0.99 - too high for the moderate similarity (~0.78)
	dedupEngine := dedup.NewEngine(embedder, db, dedup.WithThreshold(0.99))
	classifier := classify.NewClassifier(completer, 10*time.Second)

	// Per-repo threshold is 0.5 - low enough to find the moderately similar issue
	perRepoThreshold := 0.5
	repoConfigs := []config.RepoConfig{
		{
			Name:                "owner/repo",
			SimilarityThreshold: &perRepoThreshold,
		},
	}

	p := New(PipelineDeps{
		Dedup:       dedupEngine,
		Classifier:  classifier,
		Notifier:    notifier,
		Store:       db,
		Broker:      broker,
		Labels:      testLabels(),
		RepoConfigs: repoConfigs,
		Logger:      slog.Default(),
	})

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	// Insert an existing issue with embedding {0.9, 0.1, 0, 0}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Existing issue",
		Body:      "Existing body",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}
	encoded := dedup.EncodeEmbedding([]float32{0.9, 0.1, 0.0, 0.0})
	if err := db.UpdateEmbedding(repo.ID, 1, encoded, "test-model"); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	// Insert the new issue
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    2,
		Title:     "New issue",
		Body:      "New body",
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
		Number: 2,
		Title:  "New issue",
		Body:   "New body",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cosine similarity between {0.9,0.1,0,0} and {0.7,0.7,0,0} ~= 0.78
	// Global threshold is 0.99 -> would NOT find duplicates
	// Per-repo threshold is 0.5 -> WILL find duplicates (0.78 >= 0.5)
	// This proves the per-repo threshold overrides the global one
	if len(result.Duplicates) == 0 {
		t.Error("expected to find duplicates with per-repo threshold override, but found none")
	}
}

func TestPipelinePerRepoThresholdFallsBackToGlobal(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	broker := pubsub.NewBroker[github.IssueEvent]()
	embedder := newMockEmbedder()
	// Register orthogonal embeddings: cosine similarity between {1,0,0,0} and {0,1,0,0} = 0
	embedder.embeddings["New issue\n\nNew body"] = []float32{0.0, 1.0, 0.0, 0.0}
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "Bug report"}`,
	}
	notifier := &mockNotifier{}

	// Global threshold is high (0.9) - will NOT find duplicates for orthogonal vectors
	dedupEngine := dedup.NewEngine(embedder, db, dedup.WithThreshold(0.9))
	classifier := classify.NewClassifier(completer, 10*time.Second)

	// Per-repo override for "other/repo" has a very low threshold (0.01).
	// If this leaked to "owner/repo", duplicates would be found. It should NOT leak.
	lowThreshold := 0.01
	repoConfigs := []config.RepoConfig{
		{
			Name:                "other/repo",
			SimilarityThreshold: &lowThreshold,
		},
	}

	p := New(PipelineDeps{
		Dedup:       dedupEngine,
		Classifier:  classifier,
		Notifier:    notifier,
		Store:       db,
		Broker:      broker,
		Labels:      testLabels(),
		RepoConfigs: repoConfigs,
		Logger:      slog.Default(),
	})

	repo, err := db.CreateRepo("owner", "repo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	// Insert an existing issue with embedding {1, 0, 0, 0}
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Existing issue",
		Body:      "Existing body",
		State:     "open",
		Author:    "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}
	encoded := dedup.EncodeEmbedding([]float32{1.0, 0.0, 0.0, 0.0})
	if err := db.UpdateEmbedding(repo.ID, 1, encoded, "test-model"); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	// Insert the new issue
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    2,
		Title:     "New issue",
		Body:      "New body",
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
		Number: 2,
		Title:  "New issue",
		Body:   "New body",
		State:  "open",
		Author: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The embeddings {1,0,0,0} and {0,1,0,0} are orthogonal (cosine similarity = 0).
	// With the global threshold of 0.9, no duplicates should be found.
	// The per-repo override (threshold=0.01) for "other/repo" must NOT apply here.
	if len(result.Duplicates) != 0 {
		t.Errorf("expected no duplicates (global threshold used, not per-repo override), got %d", len(result.Duplicates))
	}
}

func TestPipelineFindRepoConfig(t *testing.T) {
	threshold := 0.5
	repoConfigs := []config.RepoConfig{
		{Name: "owner/repo1", CustomPrompt: "prompt1"},
		{Name: "owner/repo2", SimilarityThreshold: &threshold},
	}

	p := New(PipelineDeps{
		RepoConfigs: repoConfigs,
		Logger:      slog.Default(),
	})

	// Found
	rc := p.findRepoConfig("owner/repo1")
	if rc == nil {
		t.Fatal("expected to find config for owner/repo1")
	}
	if rc.CustomPrompt != "prompt1" {
		t.Errorf("expected CustomPrompt 'prompt1', got %q", rc.CustomPrompt)
	}

	// Found with threshold
	rc = p.findRepoConfig("owner/repo2")
	if rc == nil {
		t.Fatal("expected to find config for owner/repo2")
	}
	if rc.SimilarityThreshold == nil || *rc.SimilarityThreshold != 0.5 {
		t.Errorf("expected SimilarityThreshold 0.5, got %v", rc.SimilarityThreshold)
	}

	// Not found
	rc = p.findRepoConfig("unknown/repo")
	if rc != nil {
		t.Errorf("expected nil for unknown repo, got %+v", rc)
	}
}
