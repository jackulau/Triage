package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/store"
)

var applyCmd = &cobra.Command{
	Use:   "apply <owner/repo#number> [labels...]",
	Short: "Apply labels to an issue",
	Long: `Apply labels to a GitHub issue and log the action as an approved
human decision in the triage log.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	owner, repo, number, err := parseIssueRef(args[0])
	if err != nil {
		return err
	}
	labels := args[1:]

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

	// Apply labels via GitHub API
	_, _, err = c.GHClient.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels)
	if err != nil {
		return fmt.Errorf("applying labels to %s/%s#%d: %w", owner, repo, number, err)
	}

	fmt.Printf("Applied labels %v to %s/%s#%d\n", labels, owner, repo, number)

	// Log in triage_log
	repoRecord, err := c.Store.GetRepoByOwnerRepo(owner, repo)
	if err != nil {
		// Repo might not be in store, create it
		repoRecord, err = c.Store.CreateRepo(owner, repo)
		if err != nil {
			logger.Warn("failed to create repo record for logging", "error", err)
			return nil
		}
	}

	triageLog := &store.TriageLog{
		RepoID:          repoRecord.ID,
		IssueNumber:     number,
		Action:          "apply_labels",
		SuggestedLabels: strings.Join(labels, ", "),
		HumanDecision:   "approved",
	}

	if err := c.Store.LogTriageAction(triageLog); err != nil {
		logger.Warn("failed to log triage action", "error", err)
	} else {
		fmt.Println("Action logged as approved")
	}

	return nil
}
