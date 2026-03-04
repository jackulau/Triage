package cmd

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScanCmdArgsValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no arguments",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "too many arguments",
			args:    []string{"owner/repo", "extra"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use cobra's Args validator directly
			err := scanCmd.Args(scanCmd, tt.args)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestScanCmdExactArgs(t *testing.T) {
	// Verify scan command requires exactly 1 argument
	err := scanCmd.Args(scanCmd, []string{"owner/repo"})
	if err != nil {
		t.Errorf("expected no error with exactly 1 argument, got: %v", err)
	}
}

func TestScanRepoFormatValidation(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{
			name:    "valid owner/repo",
			repo:    "owner/repo",
			wantErr: false,
		},
		{
			name:    "missing slash",
			repo:    "ownerrepo",
			wantErr: true,
		},
		{
			name:    "only owner with slash",
			repo:    "owner/",
			wantErr: false, // SplitN will produce ["owner", ""], which is 2 parts
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the repo format validation logic used in runScan
			parts := splitRepo(tt.repo)
			if tt.wantErr && parts != nil {
				t.Error("expected invalid format, got valid")
			}
			if !tt.wantErr && parts == nil {
				t.Error("expected valid format, got invalid")
			}
		})
	}
}

// splitRepo mimics the validation in runScan/runWatch.
func splitRepo(repoArg string) []string {
	parts := make([]string, 0, 2)
	slashIdx := -1
	for i, c := range repoArg {
		if c == '/' {
			slashIdx = i
			break
		}
	}
	if slashIdx == -1 {
		return nil
	}
	parts = append(parts, repoArg[:slashIdx], repoArg[slashIdx+1:])
	return parts
}

func TestParseSinceDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantDur time.Duration
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantDur: 0,
		},
		{
			name:    "hours",
			input:   "24h",
			wantDur: 24 * time.Hour,
		},
		{
			name:    "minutes",
			input:   "30m",
			wantDur: 30 * time.Minute,
		},
		{
			name:    "days",
			input:   "7d",
			wantDur: 7 * 24 * time.Hour,
		},
		{
			name:    "one day",
			input:   "1d",
			wantDur: 24 * time.Hour,
		},
		{
			name:    "mixed duration",
			input:   "1h30m",
			wantDur: 1*time.Hour + 30*time.Minute,
		},
		{
			name:    "invalid",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "invalid days",
			input:   "abcd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSinceDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSinceDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.wantDur {
				t.Errorf("parseSinceDuration(%q) = %v, want %v", tt.input, got, tt.wantDur)
			}
		})
	}
}

func TestScanJSONArrayOutput(t *testing.T) {
	// Test that a slice of checkResultJSON marshals to a valid JSON array
	results := []checkResultJSON{
		{
			Issue:      issueJSON{Number: 1, Title: "First issue"},
			Duplicates: make([]duplicateJSON, 0),
			Labels:     []labelJSON{{Name: "bug", Confidence: 0.9}},
			Reasoning:  "looks like a bug",
		},
		{
			Issue:      issueJSON{Number: 2, Title: "Second issue"},
			Duplicates: []duplicateJSON{{Number: 1, Score: 0.85}},
			Labels:     make([]labelJSON, 0),
			Reasoning:  "",
		},
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify it parses back as an array
	var parsed []checkResultJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("parsed len = %d, want 2", len(parsed))
	}

	if parsed[0].Issue.Number != 1 {
		t.Errorf("first issue number = %d, want 1", parsed[0].Issue.Number)
	}
	if parsed[1].Issue.Number != 2 {
		t.Errorf("second issue number = %d, want 2", parsed[1].Issue.Number)
	}
	if len(parsed[1].Duplicates) != 1 {
		t.Fatalf("second issue duplicates len = %d, want 1", len(parsed[1].Duplicates))
	}
	if parsed[1].Duplicates[0].Number != 1 {
		t.Errorf("duplicate number = %d, want 1", parsed[1].Duplicates[0].Number)
	}
}

func TestScanJSONEmptyOutput(t *testing.T) {
	// Test empty results produce "[]" not "null"
	results := make([]checkResultJSON, 0)
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty results = %q, want %q", string(data), "[]")
	}
}

func TestScanFlagRegistration(t *testing.T) {
	// Verify that the scan command has the expected flags
	flags := scanCmd.Flags()

	outputFlag := flags.Lookup("output")
	if outputFlag == nil {
		t.Fatal("--output flag not found on scan command")
	}
	if outputFlag.DefValue != "text" {
		t.Errorf("--output default = %q, want %q", outputFlag.DefValue, "text")
	}

	sinceFlag := flags.Lookup("since")
	if sinceFlag == nil {
		t.Fatal("--since flag not found on scan command")
	}
	if sinceFlag.DefValue != "" {
		t.Errorf("--since default = %q, want empty", sinceFlag.DefValue)
	}

	notifyFlag := flags.Lookup("notify")
	if notifyFlag == nil {
		t.Fatal("--notify flag not found on scan command")
	}
}

func TestCheckFlagRegistration(t *testing.T) {
	// Verify that the check command has the expected flags
	flags := checkCmd.Flags()

	outputFlag := flags.Lookup("output")
	if outputFlag == nil {
		t.Fatal("--output flag not found on check command")
	}
	if outputFlag.DefValue != "text" {
		t.Errorf("--output default = %q, want %q", outputFlag.DefValue, "text")
	}
}

func TestScanCmd_DefaultWorkers(t *testing.T) {
	if defaultScanWorkers != 5 {
		t.Errorf("expected default workers to be 5, got %d", defaultScanWorkers)
	}
}

func TestScanCmd_WorkersFlagRegistered(t *testing.T) {
	flag := scanCmd.Flags().Lookup("workers")
	if flag == nil {
		t.Fatal("expected --workers flag to be registered")
	}
	if flag.DefValue != "5" {
		t.Errorf("expected default value '5', got %q", flag.DefValue)
	}
}
