package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/github"
)

var (
	watchInterval string
	watchNotify   string
	watchDryRun   bool
)

var watchCmd = &cobra.Command{
	Use:   "watch [owner/repo ...]",
	Short: "Continuously poll and triage issues",
	Long: `Watch GitHub repositories for new and updated issues.
Runs dedup detection and classification on each change, sending
results to configured notification channels.

Multiple repos can be specified as arguments:
  triage watch org/repo1 org/repo2

If no arguments are provided, all repos defined in the config file
will be watched.`,
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().StringVar(&watchInterval, "interval", "5m", "poll interval (e.g. 5m, 30s)")
	watchCmd.Flags().StringVar(&watchNotify, "notify", "", "notification target: slack, discord, or both")
	watchCmd.Flags().BoolVar(&watchDryRun, "dry-run", false, "process issues but skip notifications")
	rootCmd.AddCommand(watchCmd)
}

// parseRepoArg splits an "owner/repo" string and returns owner and repo.
func parseRepoArg(repoArg string) (owner, repo string, err error) {
	parts := strings.SplitN(repoArg, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format: expected owner/repo, got %q", repoArg)
	}
	return parts[0], parts[1], nil
}

// resolveWatchRepos determines which repos to watch from args and config.
func resolveWatchRepos(args []string, cfgRepos []string) ([]string, error) {
	if len(args) > 0 {
		// Validate all args
		for _, arg := range args {
			if _, _, err := parseRepoArg(arg); err != nil {
				return nil, err
			}
		}
		return args, nil
	}

	// No args: use repos from config
	if len(cfgRepos) == 0 {
		return nil, fmt.Errorf("no repos specified and none configured; provide repos as arguments or add them to the config file")
	}

	return cfgRepos, nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	logger := setupLogger()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Collect repo names from config
	var cfgRepoNames []string
	for _, rc := range cfg.Repos {
		if rc.Name != "" {
			cfgRepoNames = append(cfgRepoNames, rc.Name)
		}
	}

	repos, err := resolveWatchRepos(args, cfgRepoNames)
	if err != nil {
		return err
	}

	c, err := initComponents(cfg, logger)
	if err != nil {
		return fmt.Errorf("initializing components: %w", err)
	}
	defer c.Store.Close()

	// Parse interval
	interval, err := time.ParseDuration(watchInterval)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", watchInterval, err)
	}

	// Create notifier
	n, err := createNotifier(cfg, watchNotify)
	if err != nil {
		return fmt.Errorf("creating notifier: %w", err)
	}

	if watchDryRun {
		n = nil
		logger.Info("dry-run mode enabled, notifications disabled")
	}

	// Merge labels from all watched repos for the pipeline
	labels := mergeRepoLabels(cfg, repos)

	// Build pipeline (one pipeline, shared across all pollers via the broker)
	p := createPipeline(c, n, labels)

	// Create pollers for each repo
	var pollers []*github.Poller
	for _, repoArg := range repos {
		owner, repo, _ := parseRepoArg(repoArg) // already validated
		pollers = append(pollers, createPoller(c, owner, repo))
	}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	for _, repoArg := range repos {
		logger.Info("starting watch", "repo", repoArg, "interval", interval.String())
	}

	// Start pipeline in background
	pipelineErr := make(chan error, 1)
	go func() {
		pipelineErr <- p.Run(ctx)
	}()

	// Start all pollers in background
	pollerErr := make(chan error, len(pollers))
	for _, poller := range pollers {
		poller := poller // capture loop variable
		go func() {
			pollerErr <- poller.Run(ctx, interval)
		}()
	}

	// Wait for pipeline or any poller to finish
	select {
	case err := <-pipelineErr:
		cancel()
		if err != nil && err != context.Canceled {
			return fmt.Errorf("pipeline error: %w", err)
		}
	case err := <-pollerErr:
		cancel()
		if err != nil && err != context.Canceled {
			return fmt.Errorf("poller error: %w", err)
		}
	}

	logger.Info("watch stopped")
	return nil
}

// mergeRepoLabels collects labels from all specified repos, deduplicating by name.
func mergeRepoLabels(cfg *config.Config, repos []string) []config.LabelConfig {
	seen := make(map[string]bool)
	var merged []config.LabelConfig

	for _, repoArg := range repos {
		labels := findRepoLabels(cfg, repoArg)
		for _, l := range labels {
			if !seen[l.Name] {
				seen[l.Name] = true
				merged = append(merged, l)
			}
		}
	}

	return merged
}
