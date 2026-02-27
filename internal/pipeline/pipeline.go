package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jacklau/triage/internal/classify"
	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/dedup"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/notify"
	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/store"
)

// PipelineDeps holds the dependencies for the Pipeline.
type PipelineDeps struct {
	Poller     *github.Poller
	Dedup      *dedup.Engine
	Classifier *classify.Classifier
	Notifier   notify.Notifier
	Store      *store.DB
	Broker     *pubsub.Broker[github.IssueEvent]
	Labels     []config.LabelConfig
	Logger     *slog.Logger
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
func (p *Pipeline) Run(ctx context.Context) error {
	events := p.deps.Broker.Subscribe(ctx)
	p.deps.Logger.Info("pipeline started, listening for events")

	for {
		select {
		case <-ctx.Done():
			p.deps.Logger.Info("pipeline shutting down", "reason", ctx.Err())
			return ctx.Err()
		case evt, ok := <-events:
			if !ok {
				p.deps.Logger.Info("event channel closed")
				return nil
			}
			p.handleEvent(ctx, evt)
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

	result := &github.TriageResult{
		Repo:        ie.Repo,
		IssueNumber: ie.Issue.Number,
	}

	// Step 1: Run dedup
	var dedupResult *dedup.DedupResult
	if p.deps.Dedup != nil {
		dedupResult, err = p.deps.Dedup.CheckDuplicate(ctx, repo.ID, ie.Issue)
		if err != nil {
			logger.Warn("embedding/dedup failed, skipping dedup", "error", err)
			// Continue to classify
		} else {
			result.Duplicates = dedupResult.Candidates
		}
	}

	// Step 2: If not a duplicate, run classifier
	isDuplicate := dedupResult != nil && dedupResult.IsDuplicate
	if !isDuplicate && p.deps.Classifier != nil && len(p.deps.Labels) > 0 {
		classResult, err := p.deps.Classifier.Classify(ctx, ie.Repo, p.deps.Labels, ie.Issue)
		if err != nil {
			logger.Error("classification failed", "error", err)
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

	// Step 4: Send notification
	if p.deps.Notifier != nil {
		if err := p.deps.Notifier.Notify(ctx, *result); err != nil {
			logger.Warn("notification failed, retrying once", "error", err)
			// Retry once
			if err := p.deps.Notifier.Notify(ctx, *result); err != nil {
				logger.Error("notification retry failed", "error", err)
			}
		}
	}

	return result, nil
}
