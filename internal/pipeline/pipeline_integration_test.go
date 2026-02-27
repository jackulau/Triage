package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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

// capturingNotifier records all notifications it receives.
type capturingNotifier struct {
	mu      sync.Mutex
	results []github.TriageResult
}

func (n *capturingNotifier) Notify(_ context.Context, result github.TriageResult) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.results = append(n.results, result)
	return nil
}

func (n *capturingNotifier) getResults() []github.TriageResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([]github.TriageResult, len(n.results))
	copy(cp, n.results)
	return cp
}

func TestPipelineIntegration_EndToEnd(t *testing.T) {
	// Use a real SQLite database in a temp file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "triage_integration.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("opening SQLite database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Verify temp file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("expected SQLite database file to be created")
	}

	// Create a mock embedder that returns deterministic embeddings
	embedder := newMockEmbedder()
	embedder.embeddings["Test crash\n\nThe app crashes on startup"] = []float32{0.9, 0.1, 0.1, 0.1}
	embedder.embeddings["Another issue\n\nSomething else"] = []float32{0.1, 0.9, 0.1, 0.1}

	// Create a mock completer that returns realistic JSON
	completer := &mockCompleter{
		response: `{"labels": ["bug"], "confidence": 0.92, "reasoning": "This is a crash bug report describing startup failure"}`,
	}

	// Create capturing notifier
	notifier := &capturingNotifier{}

	// Create real components
	broker := pubsub.NewBroker[github.IssueEvent]()
	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	labels := []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature or request"},
		{Name: "question", Description: "Further information is requested"},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   notifier,
		Store:      db,
		Broker:     broker,
		Labels:     labels,
		Logger:     logger,
	})

	// Set up the repo and an existing issue in the database
	repo, err := db.CreateRepo("testowner", "testrepo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	now := time.Now().UTC()

	// Insert an existing issue (so dedup has something to compare against)
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    1,
		Title:     "Another issue",
		Body:      "Something else",
		State:     "open",
		Author:    "contributor",
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("upserting existing issue: %v", err)
	}

	// Insert the issue we'll be processing
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    2,
		Title:     "Test crash",
		Body:      "The app crashes on startup",
		State:     "open",
		Author:    "reporter",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("upserting target issue: %v", err)
	}

	// Start the pipeline
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	// Wait for subscription to be active
	time.Sleep(100 * time.Millisecond)

	// Publish an issue event
	broker.Publish(pubsub.Created, github.IssueEvent{
		Repo: "testowner/testrepo",
		Issue: github.Issue{
			Number: 2,
			Title:  "Test crash",
			Body:   "The app crashes on startup",
			State:  "open",
			Author: "reporter",
		},
		ChangeType: github.ChangeNew,
	})

	// Wait for the pipeline to process
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-done

	// Verify 1: Issue is stored in the database
	storedIssue, err := db.GetIssue(repo.ID, 2)
	if err != nil {
		t.Fatalf("getting stored issue: %v", err)
	}
	if storedIssue.Title != "Test crash" {
		t.Errorf("expected stored issue title 'Test crash', got %q", storedIssue.Title)
	}

	// Verify 2: Embedding was saved
	embeddings, err := db.GetEmbeddingsForRepo(repo.ID)
	if err != nil {
		t.Fatalf("getting embeddings: %v", err)
	}
	foundEmbedding := false
	for _, emb := range embeddings {
		if emb.Number == 2 {
			foundEmbedding = true
			if len(emb.Embedding) == 0 {
				t.Error("expected non-empty embedding for issue #2")
			}
			break
		}
	}
	if !foundEmbedding {
		t.Error("expected embedding for issue #2 to be stored")
	}

	// Verify 3: Classification was logged in triage_log
	triageLogs, err := db.GetTriageLog(repo.ID, 2)
	if err != nil {
		t.Fatalf("getting triage log: %v", err)
	}
	if len(triageLogs) == 0 {
		t.Fatal("expected at least one triage log entry")
	}
	logEntry := triageLogs[0]
	if logEntry.Action != "triaged" {
		t.Errorf("expected action 'triaged', got %q", logEntry.Action)
	}
	if !strings.Contains(logEntry.SuggestedLabels, "bug") {
		t.Errorf("expected suggested labels to contain 'bug', got %q", logEntry.SuggestedLabels)
	}
	if logEntry.Reasoning == "" {
		t.Error("expected non-empty reasoning in triage log")
	}

	// Verify 4: Notification was sent
	results := notifier.getResults()
	if len(results) == 0 {
		t.Fatal("expected at least one notification to be sent")
	}
	notifResult := results[0]
	if notifResult.Repo != "testowner/testrepo" {
		t.Errorf("expected repo 'testowner/testrepo', got %q", notifResult.Repo)
	}
	if notifResult.IssueNumber != 2 {
		t.Errorf("expected issue number 2, got %d", notifResult.IssueNumber)
	}
	if len(notifResult.SuggestedLabels) == 0 {
		t.Error("expected suggested labels in notification result")
	}
	if notifResult.SuggestedLabels[0].Name != "bug" {
		t.Errorf("expected first label 'bug', got %q", notifResult.SuggestedLabels[0].Name)
	}
	if notifResult.Reasoning == "" {
		t.Error("expected non-empty reasoning in notification result")
	}
}

func TestPipelineIntegration_ProcessSingleIssue(t *testing.T) {
	// Use a real SQLite temp file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "triage_single.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	embedder := newMockEmbedder()
	completer := &mockCompleter{
		response: `{"labels": ["feature"], "confidence": 0.85, "reasoning": "This is a feature request"}`,
	}
	notifier := &capturingNotifier{}
	broker := pubsub.NewBroker[github.IssueEvent]()
	dedupEngine := dedup.NewEngine(embedder, db)
	classifier := classify.NewClassifier(completer, 10*time.Second)

	labels := []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature request"},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	p := New(PipelineDeps{
		Dedup:      dedupEngine,
		Classifier: classifier,
		Notifier:   notifier,
		Store:      db,
		Broker:     broker,
		Labels:     labels,
		Logger:     logger,
	})

	// Create repo and issue
	repo, err := db.CreateRepo("myorg", "myrepo")
	if err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	now := time.Now().UTC()
	err = db.UpsertIssue(&store.Issue{
		RepoID:    repo.ID,
		Number:    5,
		Title:     "Add dark mode",
		Body:      "Please add dark mode support",
		State:     "open",
		Author:    "user1",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("upserting issue: %v", err)
	}

	ctx := context.Background()
	result, err := p.ProcessSingleIssue(ctx, "myorg/myrepo", github.Issue{
		Number: 5,
		Title:  "Add dark mode",
		Body:   "Please add dark mode support",
		State:  "open",
		Author: "user1",
	})
	if err != nil {
		t.Fatalf("ProcessSingleIssue failed: %v", err)
	}

	// Verify result
	if result.Repo != "myorg/myrepo" {
		t.Errorf("expected repo 'myorg/myrepo', got %q", result.Repo)
	}
	if result.IssueNumber != 5 {
		t.Errorf("expected issue 5, got %d", result.IssueNumber)
	}
	if len(result.SuggestedLabels) == 0 {
		t.Error("expected suggested labels")
	}

	// Verify embedding stored
	embeddings, err := db.GetEmbeddingsForRepo(repo.ID)
	if err != nil {
		t.Fatalf("getting embeddings: %v", err)
	}
	if len(embeddings) == 0 {
		t.Error("expected at least one embedding to be stored")
	}

	// Verify triage log
	logs, err := db.GetTriageLog(repo.ID, 5)
	if err != nil {
		t.Fatalf("getting triage log: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected triage log entry")
	}
	if !strings.Contains(logs[0].SuggestedLabels, "feature") {
		t.Errorf("expected 'feature' in suggested labels, got %q", logs[0].SuggestedLabels)
	}

	// Verify notification was sent
	notifResults := notifier.getResults()
	if len(notifResults) == 0 {
		t.Error("expected notification to be sent")
	}
}
