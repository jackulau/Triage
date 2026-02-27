package cmd

import (
	"testing"
	"time"
)

func TestWatchCmdArgsValidation(t *testing.T) {
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
			name:    "exactly one argument",
			args:    []string{"owner/repo"},
			wantErr: false,
		},
		{
			name:    "too many arguments",
			args:    []string{"owner/repo", "extra"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := watchCmd.Args(watchCmd, tt.args)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWatchIntervalParsing(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantDur  time.Duration
		wantErr  bool
	}{
		{
			name:     "5 minutes",
			interval: "5m",
			wantDur:  5 * time.Minute,
		},
		{
			name:     "30 seconds",
			interval: "30s",
			wantDur:  30 * time.Second,
		},
		{
			name:     "1 hour",
			interval: "1h",
			wantDur:  1 * time.Hour,
		},
		{
			name:     "complex duration",
			interval: "1h30m",
			wantDur:  1*time.Hour + 30*time.Minute,
		},
		{
			name:     "invalid duration",
			interval: "abc",
			wantErr:  true,
		},
		{
			name:     "empty string",
			interval: "",
			wantErr:  true,
		},
		{
			name:     "number without unit",
			interval: "30",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := time.ParseDuration(tt.interval)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d != tt.wantDur {
				t.Errorf("expected %v, got %v", tt.wantDur, d)
			}
		})
	}
}

func TestWatchCmdHasFlags(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		defValue string
	}{
		{
			name:     "interval flag",
			flag:     "interval",
			defValue: "5m",
		},
		{
			name:     "notify flag",
			flag:     "notify",
			defValue: "",
		},
		{
			name:     "dry-run flag",
			flag:     "dry-run",
			defValue: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := watchCmd.Flags().Lookup(tt.flag)
			if flag == nil {
				t.Fatalf("expected watch command to have --%s flag", tt.flag)
			}
			if flag.DefValue != tt.defValue {
				t.Errorf("--%s default: expected %q, got %q", tt.flag, tt.defValue, flag.DefValue)
			}
		})
	}
}

func TestWatchRepoFormatValidation(t *testing.T) {
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
			name:    "valid with org",
			repo:    "my-org/my-repo",
			wantErr: false,
		},
		{
			name:    "missing slash",
			repo:    "ownerrepo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestWatchCmdDefaultInterval(t *testing.T) {
	flag := watchCmd.Flags().Lookup("interval")
	if flag == nil {
		t.Fatal("expected watch command to have --interval flag")
	}
	d, err := time.ParseDuration(flag.DefValue)
	if err != nil {
		t.Fatalf("default interval %q is not a valid duration: %v", flag.DefValue, err)
	}
	if d != 5*time.Minute {
		t.Errorf("expected default interval 5m, got %v", d)
	}
}
