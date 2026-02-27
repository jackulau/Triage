package main

import (
	"os"

	"github.com/jacklau/triage/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
