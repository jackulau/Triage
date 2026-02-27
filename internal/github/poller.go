package github

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v60/github"

	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/store"
)

// watermarkBuffer is subtracted from the latest issue UpdatedAt to guard
// against clock skew and missed updates at page boundaries.
const watermarkBuffer = 2 * time.Minute

// Poller watches GitHub repositories for issue changes and publishes events.
type Poller struct {
	client *gogithub.Client
	store  *store.DB
	broker *pubsub.Broker[IssueEvent]
	owner  string
	repo   string
	logger *log.Logger
}

// NewPoller creates a new issue Poller for a specific repository.
func NewPoller(client *gogithub.Client, st *store.DB, broker *pubsub.Broker[IssueEvent], owner, repo string) *Poller {
	return &Poller{
		client: client,
		store:  st,
		broker: broker,
		owner:  owner,
		repo:   repo,
		logger: log.New(log.Writer(), fmt.Sprintf("[poller %s/%s] ", owner, repo), log.LstdFlags),
	}
}

// Run starts the continuous poll loop, polling at the given interval until
// the context is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) error {
	p.logger.Printf("starting poll loop with interval %s", interval)

	// Do an immediate poll
	if err := p.Poll(ctx); err != nil {
		p.logger.Printf("initial poll error: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Printf("shutting down: %v", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil {
				p.logger.Printf("poll error: %v", err)
				// Continue polling; transient errors are expected.
			}
		}
	}
}

// Poll performs a single poll cycle: fetch updated issues, diff against
// stored snapshots, publish events, and update the watermark.
func (p *Poller) Poll(ctx context.Context) error {
	// Ensure the repo record exists in the store.
	repoRecord, err := p.ensureRepo()
	if err != nil {
		return fmt.Errorf("ensuring repo record: %w", err)
	}

	// Build list options with watermark.
	opts := &gogithub.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "asc",
		ListOptions: gogithub.ListOptions{
			PerPage: 100,
		},
	}

	if repoRecord.LastPolledAt != nil {
		opts.Since = *repoRecord.LastPolledAt
	}

	var latestUpdatedAt time.Time
	var newETag string
	totalProcessed := 0

	// Paginate through all results.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		issues, resp, err := p.fetchIssuesWithRetry(ctx, opts, repoRecord.ETag)
		if err != nil {
			return fmt.Errorf("fetching issues: %w", err)
		}

		// 304 Not Modified — nothing new.
		if resp != nil && resp.StatusCode == http.StatusNotModified {
			p.logger.Printf("no changes (304 Not Modified)")
			return nil
		}

		// Capture ETag from first page response.
		if resp != nil && opts.ListOptions.Page <= 1 {
			newETag = resp.Header.Get("ETag")
		}

		// Check rate limits and throttle if needed.
		if resp != nil {
			rl := ParseRateLimit(resp.Response)
			if rl != nil && rl.ShouldThrottle() {
				wait := rl.WaitDuration()
				if wait > 0 {
					p.logger.Printf("rate limit low (remaining=%d), waiting %s", rl.Remaining, wait)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(wait):
					}
				}
			}
		}

		for _, ghIssue := range issues {
			// Skip pull requests (GitHub API returns PRs as issues).
			if ghIssue.PullRequestLinks != nil {
				continue
			}

			issue := convertIssue(ghIssue)
			changes, err := p.diffAndPublish(repoRecord.ID, issue)
			if err != nil {
				p.logger.Printf("error processing issue #%d: %v", issue.Number, err)
				continue
			}

			if len(changes) > 0 {
				totalProcessed++
			}

			if issue.UpdatedAt.After(latestUpdatedAt) {
				latestUpdatedAt = issue.UpdatedAt
			}
		}

		// Check if there are more pages.
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	// Advance watermark: latest UpdatedAt minus buffer.
	if !latestUpdatedAt.IsZero() {
		watermark := latestUpdatedAt.Add(-watermarkBuffer)
		if err := p.store.UpdatePollState(repoRecord.ID, watermark, newETag); err != nil {
			return fmt.Errorf("updating poll state: %w", err)
		}
	} else if newETag != "" {
		// No issues but got a new ETag, still save it.
		polledAt := time.Now().UTC()
		if repoRecord.LastPolledAt != nil {
			polledAt = *repoRecord.LastPolledAt
		}
		if err := p.store.UpdatePollState(repoRecord.ID, polledAt, newETag); err != nil {
			return fmt.Errorf("updating poll state: %w", err)
		}
	}

	p.logger.Printf("poll complete: processed %d issue changes", totalProcessed)
	return nil
}

// fetchIssuesWithRetry wraps the GitHub API call with retry logic for server
// errors and rate limit handling.
func (p *Poller) fetchIssuesWithRetry(ctx context.Context, opts *gogithub.IssueListByRepoOptions, etag string) ([]*gogithub.Issue, *gogithub.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := BackoffDuration(attempt - 1)
			p.logger.Printf("retrying (attempt %d/%d) after %s", attempt, maxRetries, wait)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		issues, resp, err := p.listIssuesWithETag(ctx, opts, etag)

		// Handle 304 Not Modified.
		if resp != nil && resp.StatusCode == http.StatusNotModified {
			return nil, resp, nil
		}

		// Handle rate limit errors.
		if resp != nil && IsRateLimitError(resp.Response) {
			wait, _ := HandleRateLimitError(resp.Response)
			p.logger.Printf("rate limited, waiting %s", wait)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		// Handle server errors with retry.
		if resp != nil && IsServerError(resp.Response) {
			if attempt < maxRetries {
				continue
			}
			return nil, resp, fmt.Errorf("server error after %d retries: %d", maxRetries, resp.StatusCode)
		}

		if err != nil {
			return nil, resp, err
		}

		return issues, resp, nil
	}

	return nil, nil, fmt.Errorf("exhausted retries")
}

// listIssuesWithETag calls the GitHub issues endpoint with an optional
// If-None-Match header for conditional requests.
func (p *Poller) listIssuesWithETag(ctx context.Context, opts *gogithub.IssueListByRepoOptions, etag string) ([]*gogithub.Issue, *gogithub.Response, error) {
	// Only send ETag on the first page request.
	if etag != "" && opts.ListOptions.Page <= 1 {
		// We need to use the raw HTTP client to set the ETag header.
		// However, go-github doesn't expose an easy way to do conditional
		// requests. We'll create a custom request.
		u := fmt.Sprintf("repos/%s/%s/issues", p.owner, p.repo)
		req, err := p.client.NewRequest("GET", u, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("If-None-Match", etag)

		// Add query parameters.
		q := req.URL.Query()
		q.Set("state", opts.State)
		q.Set("sort", opts.Sort)
		q.Set("direction", opts.Direction)
		q.Set("per_page", fmt.Sprintf("%d", opts.PerPage))
		if !opts.Since.IsZero() {
			q.Set("since", opts.Since.Format(time.RFC3339))
		}
		if opts.ListOptions.Page > 0 {
			q.Set("page", fmt.Sprintf("%d", opts.ListOptions.Page))
		}
		req.URL.RawQuery = q.Encode()

		var issues []*gogithub.Issue
		resp, err := p.client.Do(ctx, req, &issues)
		if err != nil {
			// go-github returns an error for non-2xx but we handle 304 ourselves.
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				return nil, resp, nil
			}
			return nil, resp, err
		}
		return issues, resp, nil
	}

	issues, resp, err := p.client.Issues.ListByRepo(ctx, p.owner, p.repo, opts)
	return issues, resp, err
}

// diffAndPublish compares the incoming issue against the stored snapshot,
// publishes events for detected changes, and upserts the snapshot.
func (p *Poller) diffAndPublish(repoID int64, issue Issue) ([]ChangeType, error) {
	bodyHash := hashBody(issue.Body)

	existing, err := p.store.GetIssue(repoID, issue.Number)
	if err != nil {
		// If not found, this is a new issue.
		if isNotFound(err) {
			existing = nil
		} else {
			return nil, fmt.Errorf("getting stored issue: %w", err)
		}
	}

	var changes []ChangeType

	if existing == nil {
		changes = append(changes, ChangeNew)
	} else {
		changes = DiffSnapshot(existing, &issue, bodyHash)
	}

	// Publish events for actionable changes.
	for _, ct := range changes {
		if ct == ChangeNew || ct == ChangeTitleEdited || ct == ChangeBodyEdited {
			evt := IssueEvent{
				Repo:       fmt.Sprintf("%s/%s", p.owner, p.repo),
				Issue:      issue,
				ChangeType: ct,
			}
			p.broker.Publish(pubsub.Created, evt)
		}
	}

	// Upsert snapshot.
	storeIssue := &store.Issue{
		RepoID:    repoID,
		Number:    issue.Number,
		Title:     issue.Title,
		Body:      issue.Body,
		BodyHash:  bodyHash,
		State:     issue.State,
		Author:    issue.Author,
		Labels:    issue.Labels,
		CreatedAt: issue.CreatedAt,
		UpdatedAt: issue.UpdatedAt,
	}
	if err := p.store.UpsertIssue(storeIssue); err != nil {
		return changes, fmt.Errorf("upserting issue: %w", err)
	}

	return changes, nil
}

// DiffSnapshot compares a stored issue against an incoming issue and returns
// which fields changed. It uses SHA-256 of the body for efficient comparison.
func DiffSnapshot(stored *store.Issue, incoming *Issue, incomingBodyHash string) []ChangeType {
	var changes []ChangeType

	if stored.Title != incoming.Title {
		changes = append(changes, ChangeTitleEdited)
	}

	if stored.BodyHash != incomingBodyHash {
		changes = append(changes, ChangeBodyEdited)
	}

	if stored.State != incoming.State {
		changes = append(changes, ChangeStateChanged)
	}

	if !labelsEqual(stored.Labels, incoming.Labels) {
		changes = append(changes, ChangeLabelsChanged)
	}

	return changes
}

// ensureRepo gets or creates the repo record in the store.
func (p *Poller) ensureRepo() (*store.Repo, error) {
	repo, err := p.store.GetRepoByOwnerRepo(p.owner, p.repo)
	if err != nil {
		if isNotFound(err) {
			return p.store.CreateRepo(p.owner, p.repo)
		}
		return nil, err
	}
	return repo, nil
}

// convertIssue converts a go-github Issue to our internal Issue type.
func convertIssue(gh *gogithub.Issue) Issue {
	issue := Issue{
		Number: gh.GetNumber(),
		Title:  gh.GetTitle(),
		Body:   gh.GetBody(),
		State:  gh.GetState(),
	}

	if gh.User != nil {
		issue.Author = gh.User.GetLogin()
	}

	for _, label := range gh.Labels {
		issue.Labels = append(issue.Labels, label.GetName())
	}

	if gh.CreatedAt != nil {
		issue.CreatedAt = gh.CreatedAt.Time
	}
	if gh.UpdatedAt != nil {
		issue.UpdatedAt = gh.UpdatedAt.Time
	}

	return issue
}

// hashBody returns the hex-encoded SHA-256 hash of the body text.
func hashBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// labelsEqual returns true if two label slices contain the same labels
// (order-independent).
func labelsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := make([]string, len(a))
	copy(sortedA, a)
	sort.Strings(sortedA)

	sortedB := make([]string, len(b))
	copy(sortedB, b)
	sort.Strings(sortedB)

	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

// isNotFound checks if an error is a "not found" type error from the store.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err == sql.ErrNoRows || strings.Contains(err.Error(), "no rows")
}
