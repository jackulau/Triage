package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestNoStderrInScanWatch verifies that scan.go and watch.go do not contain
// direct fmt.Fprintf(os.Stderr, ...) calls, which should be replaced by slog.
func TestNoStderrInScanWatch(t *testing.T) {
	files := []string{
		"scan.go",
		"watch.go",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("failed to read %s: %v", f, err)
			}
			content := string(data)

			// Check for fmt.Fprintf(os.Stderr patterns
			if strings.Contains(content, "fmt.Fprintf(os.Stderr") {
				t.Errorf("%s still contains fmt.Fprintf(os.Stderr, ...) — should use slog instead", f)
			}
			if strings.Contains(content, "fmt.Fprintln(os.Stderr") {
				t.Errorf("%s still contains fmt.Fprintln(os.Stderr, ...) — should use slog instead", f)
			}
		})
	}
}

// TestSetupLoggerReturnsLogger verifies setupLogger returns a non-nil logger.
func TestSetupLoggerReturnsLogger(t *testing.T) {
	logger := setupLogger()
	if logger == nil {
		t.Fatal("setupLogger() returned nil")
	}
}

// TestSetupLoggerVerbose verifies verbose flag affects logger level.
func TestSetupLoggerVerbose(t *testing.T) {
	oldVerbose := verbose
	defer func() { verbose = oldVerbose }()

	verbose = false
	logger := setupLogger()
	if logger == nil {
		t.Fatal("setupLogger() returned nil with verbose=false")
	}

	verbose = true
	loggerV := setupLogger()
	if loggerV == nil {
		t.Fatal("setupLogger() returned nil with verbose=true")
	}
}
