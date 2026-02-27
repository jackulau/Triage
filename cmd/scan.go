package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/notify"
	"github.com/jacklau/triage/internal/store"

	gogithub "github.com/google/go-github/v60/github"
)

var (
	scanNotify  string
	scanOutput  string
	scanSince   string
	scanWorkers int
)

const defaultScanWorkers = 5

var scanCmd = &cobra.Command{
	Use:   "scan <owner/repo>",
	Short: "One-shot full scan of all open issues",
	Long: `Scan fetches all open issues from a repository, computes embeddings,
runs dedup detection across all issues, classifies unlabeled issues,
and sends a summary notification.

Use --since to limit scanning to recently updated issues (e.g. --since 24h).
Use --output json to get structured JSON output.`,
	Args: cobra.ExactArgs(1),
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVar(&scanNotify, "notify", "", "notification target: slack, discord, or both")
	scanCmd.Flags().StringVar(&scanOutput, "output", "text", "output format: text or json")
	scanCmd.Flags().StringVar(&scanSince, "since", "", "only process issues updated within this duration (e.g. 24h, 7d)")
	scanCmd.Flags().IntVar(&scanWorkers, "workers", defaultScanWorkers, "number of concurrent workers for issue processing")
	rootCmd.AddCommand(scanCmd)
}

// parseSinceDuration parses a duration string that supports standard Go duration
// syntax plus a "d" suffix for days (e.g. "7d" = 7*24h).
func parseSinceDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	// Support "d" suffix for days
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		days, err := time.ParseDuration(numStr + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return days * 24, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

func runScan(cmd *cobra.Command, args []string) error {
	repoArg := args[0]
	parts := strings.SplitN(repoArg, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format: expected owner/repo, got %q", repoArg)
	}
	owner, repo := parts[0], parts[1]

	// Parse --since flag
	sinceDuration, err := parseSinceDuration(scanSince)
	if err != nil {
		return err
	}

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

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Create or get repo record
	repoRecord, err := c.Store.GetRepoByOwnerRepo(owner, repo)
	if err != nil {
		repoRecord, err = c.Store.CreateRepo(owner, repo)
		if err != nil {
			return fmt.Errorf("creating repo record: %w", err)
		}
	}

	// Fetch all open issues with pagination
	logger.Info("fetching open issues", "owner", owner, "repo", repo)

	var allIssues []github.Issue
	opts := &gogithub.IssueListByRepoOptions{
		State:     "open",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gogithub.ListOptions{
			PerPage: 100,
		},
	}

	// Apply --since filter at the API level
	if sinceDuration > 0 {
		opts.Since = time.Now().Add(-sinceDuration)
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
			issue := convertGHIssue(ghIssue)

			// Client-side filter for --since (in case API doesn't filter precisely)
			if sinceDuration > 0 {
				cutoff := time.Now().Add(-sinceDuration)
				if issue.UpdatedAt.Before(cutoff) {
					continue
				}
			}

			allIssues = append(allIssues, issue)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	total := len(allIssues)
	logger.Info("found open issues", "count", total)
	if sinceDuration > 0 {
		fmt.Fprintf(os.Stderr, "Found %d open issues updated within %s\n", total, scanSince)
	} else {
		fmt.Fprintf(os.Stderr, "Found %d open issues\n", total)
	}

	if total == 0 {
		if scanOutput == "json" {
			fmt.Println("[]")
		} else {
			fmt.Println("No open issues found.")
		}
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

	var triaged, duplicatesCount, classifiedCount int64
	var mu sync.Mutex
	var results []checkResultJSON
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	bar := newProgressBar(total, "Processing", os.Stderr)

	for _, issue := range allIssues {
		wg.Add(1)
		sem <- struct{}{}
		go func(iss github.Issue) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := p.ProcessSingleIssue(ctx, repoArg, iss)
			bar.Add(1)

			if err != nil {
				logger.Warn("failed to process issue", "issue", iss.Number, "error", err)
				return
			}

			atomic.AddInt64(&triaged, 1)
			if len(result.Duplicates) > 0 {
				atomic.AddInt64(&duplicatesCount, 1)
			}
			if len(result.SuggestedLabels) > 0 {
				atomic.AddInt64(&classifiedCount, 1)
			}

			if scanOutput == "json" {
				jr := checkResultJSON{
					Issue: issueJSON{
						Number: iss.Number,
						Title:  iss.Title,
					},
					Duplicates: make([]duplicateJSON, 0, len(result.Duplicates)),
					Labels:     make([]labelJSON, 0, len(result.SuggestedLabels)),
					Reasoning:  result.Reasoning,
				}
				for _, d := range result.Duplicates {
					jr.Duplicates = append(jr.Duplicates, duplicateJSON{
						Number: d.Number,
						Score:  float64(d.Score),
					})
				}
				for _, l := range result.SuggestedLabels {
					jr.Labels = append(jr.Labels, labelJSON{
						Name:       l.Name,
						Confidence: l.Confidence,
					})
				}
				mu.Lock()
				results = append(results, jr)
				mu.Unlock()
			}
		}(issue)
	}
	wg.Wait()
	bar.Finish()

	// Output results
	dupCount := atomic.LoadInt64(&duplicatesCount)
	classCount := atomic.LoadInt64(&classifiedCount)
	triagedCount := atomic.LoadInt64(&triaged)

	if scanOutput == "json" {
		if results == nil {
			results = make([]checkResultJSON, 0)
		}
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		fmt.Println(string(data))
	} else {
		// Print text summary
		fmt.Printf("\nScan complete for %s/%s\n", owner, repo)
		fmt.Printf("  Total issues scanned: %d\n", total)
		fmt.Printf("  Successfully triaged: %d\n", triagedCount)
		fmt.Printf("  Potential duplicates: %d\n", dupCount)
		fmt.Printf("  Issues classified:    %d\n", classCount)
	}

	// Send summary notification
	if n != nil {
		summaryResult := github.TriageResult{
			Repo:        repoArg,
			IssueNumber: 0, // summary, not a single issue
			Reasoning:   fmt.Sprintf("Scan complete: %d issues scanned, %d potential duplicates, %d classified", total, dupCount, classCount),
		}
		if err := n.Notify(ctx, summaryResult); err != nil {
			logger.Warn("failed to send summary notification", "error", err)
		}
	}

	return nil
}

// noopNotifier is a Notifier that does nothing.
type noopNotifier struct{}

func (n *noopNotifier) Notify(_ context.Context, _ github.TriageResult) error { return nil }

var _ notify.Notifier = (*noopNotifier)(nil)
