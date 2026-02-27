package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "triage",
	Short: "Watch GitHub repos for new issues and triage them with AI",
	Long: `Triage watches GitHub repositories for new issues, detects duplicates
via AI embeddings, classifies them with LLMs, and sends results to
Slack/Discord for human review.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file (default %s)", defaultConfigPath()))
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".triage/config.yaml"
	}
	return home + "/.triage/config.yaml"
}
