package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is set at build time via ldflags:
//
//	go build -ldflags="-X github.com/jacklau/triage/cmd.version=1.0.0"
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of triage",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "triage", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
