package cmd

import (
	"testing"
	"time"
)

func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		t        time.Time
		expected string
	}{
		{
			name:     "just now",
			t:        time.Now().Add(-10 * time.Second),
			expected: "just now",
		},
		{
			name:     "1 minute ago",
			t:        time.Now().Add(-1 * time.Minute),
			expected: "1 minute ago",
		},
		{
			name:     "5 minutes ago",
			t:        time.Now().Add(-5 * time.Minute),
			expected: "5 minutes ago",
		},
		{
			name:     "1 hour ago",
			t:        time.Now().Add(-1 * time.Hour),
			expected: "1 hour ago",
		},
		{
			name:     "3 hours ago",
			t:        time.Now().Add(-3 * time.Hour),
			expected: "3 hours ago",
		},
		{
			name:     "1 day ago",
			t:        time.Now().Add(-24 * time.Hour),
			expected: "1 day ago",
		},
		{
			name:     "7 days ago",
			t:        time.Now().Add(-7 * 24 * time.Hour),
			expected: "7 days ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeAgo(tt.t)
			if result != tt.expected {
				t.Errorf("formatTimeAgo() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"small", 512, "512 B"},
		{"1KB", 1024, "1.0 KB"},
		{"1.5KB", 1536, "1.5 KB"},
		{"1MB", 1024 * 1024, "1.0 MB"},
		{"1GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"2.5MB", 2621440, "2.5 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestDbFileSize_NonExistent(t *testing.T) {
	_, err := dbFileSize("/nonexistent/path/to/db.sqlite")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
