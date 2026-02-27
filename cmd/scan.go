package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/notify"
	"github.com/jacklau/triage/internal/store"

	gogithub "github.com/google/go-github/v60/github"
)

var (
	scanNotify  string
	scanWorkers int
)

const defaultScanWorkers = 5

var scanCmd = &cobra.Command{
	Use:   "scan <owner/repo>",
	Short: "One-shot full scan of all open issues",
	Long: `Scan fetches all open issues from a repository, computes embeddings,
runs dedup detection across all issues, classifies unlabeled issues,
and sends a summary notification.`,
	Args: cobra.ExactArgs(1),
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVar(&scanNotify, "notify", "", "notification target: slack, discord, or both")
	scanCmd.Flags().IntVar(&scanWorkers, "workers", defaultScanWorkers, "number of concurrent workers for issue processing")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	repoArg := args[0]
	parts := strings.SplitN(repoArg, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format: expected owner/repo, got %q", repoArg)
	}
	owner, repo := parts[0], parts[1]

	logger := setupLogger()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	c, err := initComponents(cfg, logger)
	if err != nil {
		return fmt.Errorf("initializing components: %w", err)
	}
	defer c.Store.Close()

	if c.GHClient == nil {
		return fmt.Errorf("GitHub client not configured (set github.auth: app in config)")
	}

	ctx := context.Background()

	// Create or get repo record
	repoRecord, err := c.Store.GetRepoByOwnerRepo(owner, repo)
	if err != nil {
		repoRecord, err = c.Store.CreateRepo(owner, repo)
		if err != nil {
			return fmt.Errorf("creating repo record: %w", err)
		}
	}

	// Fetch all open issues with pagination
	fmt.Fprintf(os.Stderr, "Fetching open issues from %s/%s...\n", owner, repo)

	var allIssues []github.Issue
	opts := &gogithub.IssueListByRepoOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gogithub.ListOptions{
			PerPage: 100,
		},
	}

	for {
		issues, resp, err := c.GHClient.Issues.ListByRepo(ctx, owner, repo, opts)
		if err != nil {
			return fmt.Errorf("fetching issues: %w", err)
		}

		for _, ghIssue := range issues {
			if ghIssue.PullRequestLinks != nil {
				continue // skip PRs
			}
			allIssues = append(allIssues, convertGHIssue(ghIssue))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	total := len(allIssues)
	fmt.Fprintf(os.Stderr, "Found %d open issues\n", total)

	if total == 0 {
		fmt.Println("No open issues found.")
		return nil
	}

	// Upsert all issues into store
	for _, issue := range allIssues {
		err := c.Store.UpsertIssue(&store.Issue{
			RepoID:    repoRecord.ID,
			Number:    issue.Number,
			Title:     issue.Title,
			Body:      issue.Body,
			State:     issue.State,
			Author:    issue.Author,
			Labels:    issue.Labels,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
		})
		if err != nil {
			logger.Warn("failed to upsert issue", "issue", issue.Number, "error", err)
		}
	}

	// Build pipeline for single-issue processing
	labels := findRepoLabels(cfg, repoArg)
	n, err := createNotifier(cfg, scanNotify)
	if err != nil {
		logger.Warn("failed to create notifier", "error", err)
	}
	p := createPipeline(c, n, labels)

	// Process issues concurrently using a worker pool
	workers := scanWorkers
	if workers <= 0 {
		workers = defaultScanWorkers
	}

	var triaged, duplicates, classified int64
	var processed int64
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, issue := range allIssues {
		wg.Add(1)
		sem <- struct{}{}
		go func(iss github.Issue) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := p.ProcessSingleIssue(ctx, repoArg, iss)
			count := atomic.AddInt64(&processed, 1)
			fmt.Fprintf(os.Stderr, "\rProcessing... %d/%d", count, total)

			if err != nil {
				logger.Warn("failed to process issue", "issue", iss.Number, "error", err)
				return
			}

			atomic.AddInt64(&triaged, 1)
			if len(result.Duplicates) > 0 {
				atomic.AddInt64(&duplicates, 1)
			}
			if len(result.SuggestedLabels) > 0 {
				atomic.AddInt64(&classified, 1)
			}
		}(issue)
	}
	wg.Wait()
	fmt.Fprintln(os.Stderr) // newline after progress

	// Print summary
	triagedCount := atomic.LoadInt64(&triaged)
	duplicatesCount := atomic.LoadInt64(&duplicates)
	classifiedCount := atomic.LoadInt64(&classified)

	fmt.Printf("\nScan complete for %s/%s\n", owner, repo)
	fmt.Printf("  Total issues scanned: %d\n", total)
	fmt.Printf("  Successfully triaged: %d\n", triagedCount)
	fmt.Printf("  Potential duplicates: %d\n", duplicatesCount)
	fmt.Printf("  Issues classified:    %d\n", classifiedCount)

	// Send summary notification
	if n != nil {
		summaryResult := github.TriageResult{
			Repo:        repoArg,
			IssueNumber: 0, // summary, not a single issue
			Reasoning:   fmt.Sprintf("Scan complete: %d issues scanned, %d potential duplicates, %d classified", total, duplicatesCount, classifiedCount),
		}
		if err := n.Notify(ctx, summaryResult); err != nil {
			logger.Warn("failed to send summary notification", "error", err)
		}
	}

	return nil
}

func convertGHIssue(gh *gogithub.Issue) github.Issue {
	issue := github.Issue{
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

// noopNotifier is a Notifier that does nothing.
type noopNotifier struct{}

func (n *noopNotifier) Notify(_ context.Context, _ github.TriageResult) error { return nil }

var _ notify.Notifier = (*noopNotifier)(nil)
