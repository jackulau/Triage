package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jacklau/triage/internal/classify"
	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/dedup"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/notify"
	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/retry"
	"github.com/jacklau/triage/internal/store"
)

const (
	// drainTimeout is the maximum time allowed for an in-flight event to
	// complete during graceful shutdown.
	drainTimeout = 30 * time.Second
)

// PipelineDeps holds the dependencies for the Pipeline.
type PipelineDeps struct {
	Poller      *github.Poller
	Dedup       *dedup.Engine
	Classifier  *classify.Classifier
	Notifier    notify.Notifier
	Store       *store.DB
	Broker      *pubsub.Broker[github.IssueEvent]
	Labels      []config.LabelConfig
	RepoConfigs []config.RepoConfig
	Logger      *slog.Logger
}

// Pipeline orchestrates the issue triage workflow: dedup, classify, notify.
type Pipeline struct {
	deps PipelineDeps
}

// New creates a new Pipeline with the given dependencies.
func New(deps PipelineDeps) *Pipeline {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Pipeline{deps: deps}
}

// Run subscribes to the broker and processes IssueEvents until the context is cancelled.
// When the context is cancelled, Run waits for the current in-flight event to finish
// processing before returning, ensuring graceful shutdown. In-flight events use a
// detached context so they are not interrupted by pipeline cancellation.
func (p *Pipeline) Run(ctx context.Context) error {
	events := p.deps.Broker.Subscribe(ctx)
	p.deps.Logger.Info("pipeline started, listening for events")

	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			p.deps.Logger.Info("pipeline shutting down, waiting for in-flight events", "reason", ctx.Err())
			wg.Wait()
			p.deps.Logger.Info("pipeline shutdown complete")
			return ctx.Err()
		case evt, ok := <-events:
			if !ok {
				p.deps.Logger.Info("event channel closed, waiting for in-flight events")
				wg.Wait()
				p.deps.Logger.Info("pipeline shutdown complete")
				return nil
			}
			wg.Add(1)
			// Use a detached context with a timeout for processing so that
			// in-flight events are not interrupted by pipeline context
			// cancellation but still have a bounded lifetime.
			processCtx, processCancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				drainTimeout,
			)
			p.handleEvent(processCtx, evt)
			processCancel()
			wg.Done()
		}
	}
}

func (p *Pipeline) handleEvent(ctx context.Context, evt pubsub.Event[github.IssueEvent]) {
	ie := evt.Payload

	// Only process actionable change types
	switch ie.ChangeType {
	case github.ChangeNew, github.ChangeTitleEdited, github.ChangeBodyEdited:
		// proceed
	default:
		return
	}

	logger := p.deps.Logger.With(
		"repo", ie.Repo,
		"issue", ie.Issue.Number,
		"change", ie.ChangeType.String(),
	)

	start := time.Now()
	logger.Info("processing issue")

	result, err := p.processIssue(ctx, ie, logger)
	if err != nil {
		logger.Error("failed to process issue", "error", err, "duration", time.Since(start))
		return
	}

	logger.Info("issue processed",
		"duplicates", len(result.Duplicates),
		"labels", len(result.SuggestedLabels),
		"duration", time.Since(start),
	)
}

// ProcessSingleIssue exposes processing a single issue for use by scan/check commands.
func (p *Pipeline) ProcessSingleIssue(ctx context.Context, repo string, issue github.Issue) (*github.TriageResult, error) {
	logger := p.deps.Logger.With("repo", repo, "issue", issue.Number)
	ie := github.IssueEvent{
		Repo:       repo,
		Issue:      issue,
		ChangeType: github.ChangeNew,
	}
	return p.processIssue(ctx, ie, logger)
}

// findRepoConfig looks up the RepoConfig for the given full repo name (owner/repo).
// Returns nil if no per-repo config is found.
func (p *Pipeline) findRepoConfig(repoFullName string) *config.RepoConfig {
	for i := range p.deps.RepoConfigs {
		if p.deps.RepoConfigs[i].Name == repoFullName {
			return &p.deps.RepoConfigs[i]
		}
	}
	return nil
}

func (p *Pipeline) processIssue(ctx context.Context, ie github.IssueEvent, logger *slog.Logger) (*github.TriageResult, error) {
	parts := strings.SplitN(ie.Repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", ie.Repo)
	}
	owner, repoName := parts[0], parts[1]

	// Get or create repo record
	repo, err := p.deps.Store.GetRepoByOwnerRepo(owner, repoName)
	if err != nil {
		repo, err = p.deps.Store.CreateRepo(owner, repoName)
		if err != nil {
			return nil, fmt.Errorf("creating repo record: %w", err)
		}
	}

	// Look up per-repo config overrides
	rc := p.findRepoConfig(ie.Repo)

	result := &github.TriageResult{
		Repo:        ie.Repo,
		IssueNumber: ie.Issue.Number,
	}

	// Step 1: Run dedup with retry and optional per-repo threshold
	var dedupResult *dedup.DedupResult
	if p.deps.Dedup != nil {
		var thresholdOverride float32
		if rc != nil && rc.SimilarityThreshold != nil {
			thresholdOverride = float32(*rc.SimilarityThreshold)
		}
		retryErr := retry.Do(ctx, retry.DefaultMaxAttempts, func() error {
			var dedupErr error
			dedupResult, dedupErr = p.deps.Dedup.CheckDuplicateWithThreshold(ctx, repo.ID, ie.Issue, thresholdOverride)
			return dedupErr
		})
		if retryErr != nil {
			logger.Warn("embedding/dedup failed after retries, skipping dedup", "error", retryErr)
			// Continue to classify
		} else {
			result.Duplicates = dedupResult.Candidates
		}
	}

	// Step 2: If not a duplicate, run classifier with retry and optional custom prompt
	isDuplicate := dedupResult != nil && dedupResult.IsDuplicate
	if !isDuplicate && p.deps.Classifier != nil && len(p.deps.Labels) > 0 {
		var customPrompt string
		if rc != nil {
			customPrompt = rc.CustomPrompt
		}
		var classResult *classify.ClassifyResult
		retryErr := retry.Do(ctx, retry.DefaultMaxAttempts, func() error {
			var classErr error
			classResult, classErr = p.deps.Classifier.ClassifyWithCustomPrompt(ctx, ie.Repo, p.deps.Labels, ie.Issue, customPrompt)
			return classErr
		})
		if retryErr != nil {
			logger.Error("classification failed after retries", "error", retryErr)
			// Send notification with dedup results only
		} else {
			result.SuggestedLabels = classResult.Labels
			result.Reasoning = classResult.Reasoning
		}
	}

	// Step 3: Log in triage_log
	action := "triaged"
	if isDuplicate {
		action = "duplicate"
	}

	duplicateOf := ""
	if len(result.Duplicates) > 0 {
		dupParts := make([]string, len(result.Duplicates))
		for i, d := range result.Duplicates {
			dupParts[i] = fmt.Sprintf("#%d", d.Number)
		}
		duplicateOf = strings.Join(dupParts, ", ")
	}

	labelNames := make([]string, len(result.SuggestedLabels))
	for i, l := range result.SuggestedLabels {
		labelNames[i] = l.Name
	}

	triageLog := &store.TriageLog{
		RepoID:          repo.ID,
		IssueNumber:     ie.Issue.Number,
		Action:          action,
		DuplicateOf:     duplicateOf,
		SuggestedLabels: strings.Join(labelNames, ", "),
		Reasoning:       result.Reasoning,
	}

	if err := p.deps.Store.LogTriageAction(triageLog); err != nil {
		logger.Error("failed to log triage action", "error", err)
	}

	// Step 4: Send notification with retry
	if p.deps.Notifier != nil {
		notifyErr := retry.Do(ctx, retry.DefaultMaxAttempts, func() error {
			return p.deps.Notifier.Notify(ctx, *result)
		})
		if notifyErr != nil {
			logger.Error("notification failed after retries", "error", notifyErr)
		}
	}

	return result, nil
}
