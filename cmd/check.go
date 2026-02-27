package cmd

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/store"

	gogithub "github.com/google/go-github/v60/github"
)

var checkCmd = &cobra.Command{
	Use:   "check <owner/repo#number>",
	Short: "Check a single issue for duplicates and classification",
	Long: `Check fetches a single issue, runs dedup detection and classification,
and prints the results to stdout.`,
	Args: cobra.ExactArgs(1),
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func parseIssueRef(ref string) (owner, repo string, number int, err error) {
	// Format: owner/repo#number
	hashIdx := strings.LastIndex(ref, "#")
	if hashIdx == -1 {
		return "", "", 0, fmt.Errorf("invalid format: expected owner/repo#number, got %q", ref)
	}

	repoFull := ref[:hashIdx]
	numStr := ref[hashIdx+1:]

	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("invalid repo format: expected owner/repo, got %q", repoFull)
	}

	number, err = strconv.Atoi(numStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid issue number %q: %w", numStr, err)
	}

	return parts[0], parts[1], number, nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	owner, repo, number, err := parseIssueRef(args[0])
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

	ctx := context.Background()

	// Fetch the issue
	ghIssue, _, err := c.GHClient.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return fmt.Errorf("fetching issue #%d: %w", number, err)
	}

	issue := convertGHIssuePtr(ghIssue)

	// Ensure repo and issue exist in store
	repoRecord, err := c.Store.GetRepoByOwnerRepo(owner, repo)
	if err != nil {
		repoRecord, err = c.Store.CreateRepo(owner, repo)
		if err != nil {
			return fmt.Errorf("creating repo record: %w", err)
		}
	}

	err = c.Store.UpsertIssue(&store.Issue{
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
		logger.Warn("failed to upsert issue", "error", err)
	}

	// Run pipeline without notifier
	repoFull := fmt.Sprintf("%s/%s", owner, repo)
	labels := findRepoLabels(cfg, repoFull)
	p := createPipeline(c, nil, labels)

	result, err := p.ProcessSingleIssue(ctx, repoFull, issue)
	if err != nil {
		return fmt.Errorf("processing issue: %w", err)
	}

	// Print results
	fmt.Printf("Issue: %s#%d\n", repoFull, number)
	fmt.Printf("Title: %s\n", issue.Title)
	fmt.Printf("State: %s\n", issue.State)
	fmt.Printf("Author: %s\n", issue.Author)
	if len(issue.Labels) > 0 {
		fmt.Printf("Current Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	fmt.Println()

	// Duplicates
	fmt.Println("Duplicate Detection:")
	if len(result.Duplicates) == 0 {
		fmt.Println("  No duplicates found")
	} else {
		for _, d := range result.Duplicates {
			pct := int(math.Round(float64(d.Score) * 100))
			fmt.Printf("  #%d — %d%% similar\n", d.Number, pct)
		}
	}
	fmt.Println()

	// Classification
	fmt.Println("Classification:")
	if len(result.SuggestedLabels) == 0 {
		fmt.Println("  No labels suggested")
	} else {
		for _, l := range result.SuggestedLabels {
			pct := int(math.Round(l.Confidence * 100))
			fmt.Printf("  %s (%d%% confidence)\n", l.Name, pct)
		}
	}

	if result.Reasoning != "" {
		fmt.Printf("\nReasoning: %s\n", result.Reasoning)
	}

	return nil
}

func convertGHIssuePtr(gh *gogithub.Issue) github.Issue {
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
