package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	oldVersion := version
	version = "test-1.2.3"
	defer func() { version = oldVersion }()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	}()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	want := "triage test-1.2.3"
	if got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
}

func TestVersionDefaultIsDev(t *testing.T) {
	oldVersion := version
	version = "dev"
	defer func() { version = oldVersion }()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	}()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	want := "triage dev"
	if got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
}

func TestVersionCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("version command not registered on rootCmd")
	}
}

func TestVersionLdflags(t *testing.T) {
	// Verify the version variable can be overridden (simulating ldflags)
	oldVersion := version
	defer func() { version = oldVersion }()

	testCases := []struct {
		name    string
		ver     string
		wantOut string
	}{
		{"semver", "1.0.0", "triage 1.0.0"},
		{"commit sha", "abc123", "triage abc123"},
		{"dev default", "dev", "triage dev"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			version = tc.ver

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetArgs([]string{"version"})
			defer func() {
				rootCmd.SetOut(nil)
				rootCmd.SetArgs(nil)
			}()

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("version command failed: %v", err)
			}

			got := strings.TrimSpace(buf.String())
			if got != tc.wantOut {
				t.Errorf("got %q, want %q", got, tc.wantOut)
			}
		})
	}
}
