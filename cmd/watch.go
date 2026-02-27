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
)

var (
	watchInterval string
	watchNotify   string
	watchDryRun   bool
)

var watchCmd = &cobra.Command{
	Use:   "watch <owner/repo>",
	Short: "Continuously poll and triage issues",
	Long: `Watch a GitHub repository for new and updated issues.
Runs dedup detection and classification on each change, sending
results to configured notification channels.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().StringVar(&watchInterval, "interval", "5m", "poll interval (e.g. 5m, 30s)")
	watchCmd.Flags().StringVar(&watchNotify, "notify", "", "notification target: slack, discord, or both")
	watchCmd.Flags().BoolVar(&watchDryRun, "dry-run", false, "process issues but skip notifications")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
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

	// Build pipeline
	labels := findRepoLabels(cfg, repoArg)
	p := createPipeline(c, n, labels)
	poller := createPoller(c, owner, repo)

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

	fmt.Fprintf(os.Stderr, "[%s] Starting watch on %s/%s (interval: %s)\n",
		time.Now().Format(time.RFC3339), owner, repo, interval)

	// Start pipeline in background
	pipelineErr := make(chan error, 1)
	go func() {
		pipelineErr <- p.Run(ctx)
	}()

	// Start poller (blocks until cancelled)
	pollerErr := make(chan error, 1)
	go func() {
		pollerErr <- poller.Run(ctx, interval)
	}()

	// Wait for either to finish
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

	fmt.Fprintf(os.Stderr, "[%s] Watch stopped\n", time.Now().Format(time.RFC3339))
	return nil
}
