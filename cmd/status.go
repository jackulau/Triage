package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show store and pipeline health overview",
	Long: `Display statistics about tracked repositories including issue counts,
embedding counts, classification counts, last poll times, and database size.`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	// Get stats for all repos
	allStats, err := c.Store.GetAllRepoStats()
	if err != nil {
		return fmt.Errorf("querying stats: %w", err)
	}

	if len(allStats) == 0 {
		fmt.Println("No repositories tracked yet.")
		fmt.Println("Run 'triage watch <owner/repo>' or 'triage scan <owner/repo>' to get started.")
		return nil
	}

	// Print per-repo stats
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPOSITORY\tISSUES\tEMBEDDINGS\tCLASSIFIED\tLAST POLLED")
	fmt.Fprintln(w, "----------\t------\t----------\t----------\t-----------")

	var totalIssues, totalEmbeddings, totalClassified int
	for _, s := range allStats {
		repoName := fmt.Sprintf("%s/%s", s.Repo.Owner, s.Repo.RepoName)
		lastPolled := "never"
		if s.Repo.LastPolledAt != nil {
			lastPolled = formatTimeAgo(*s.Repo.LastPolledAt)
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\n",
			repoName, s.IssueCount, s.EmbeddingCount, s.ClassifiedCount, lastPolled)

		totalIssues += s.IssueCount
		totalEmbeddings += s.EmbeddingCount
		totalClassified += s.ClassifiedCount
	}

	if len(allStats) > 1 {
		fmt.Fprintf(w, "TOTAL\t%d\t%d\t%d\t\n",
			totalIssues, totalEmbeddings, totalClassified)
	}
	w.Flush()

	// Print database file size
	fmt.Println()
	dbSize, err := dbFileSize(cfg.Store.Path)
	if err != nil {
		fmt.Printf("Database: %s (size unknown)\n", cfg.Store.Path)
	} else {
		fmt.Printf("Database: %s (%s)\n", cfg.Store.Path, formatBytes(dbSize))
	}

	return nil
}

// formatTimeAgo formats a time as a human-readable relative string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// formatBytes formats bytes into a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// dbFileSize returns the size in bytes of the database file.
func dbFileSize(path string) (int64, error) {
	// Expand ~ in path
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		path = home + path[1:]
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
