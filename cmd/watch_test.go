package cmd

import (
	"testing"

	"github.com/jacklau/triage/internal/config"
)

func TestParseRepoArg(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"valid org", "my-org/my-repo", "my-org", "my-repo", false},
		{"no slash", "invalid", "", "", true},
		{"empty owner", "/repo", "", "", true},
		{"empty repo", "owner/", "", "", true},
		{"empty string", "", "", "", true},
		{"triple slash", "a/b/c", "a", "b/c", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRepoArg(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRepoArg(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("parseRepoArg(%q) owner = %q, want %q", tt.input, owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("parseRepoArg(%q) repo = %q, want %q", tt.input, repo, tt.wantRepo)
				}
			}
		})
	}
}

func TestResolveWatchRepos_WithArgs(t *testing.T) {
	args := []string{"org/repo1", "org/repo2"}
	repos, err := resolveWatchRepos(args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0] != "org/repo1" || repos[1] != "org/repo2" {
		t.Errorf("unexpected repos: %v", repos)
	}
}

func TestResolveWatchRepos_InvalidArgs(t *testing.T) {
	args := []string{"org/repo1", "invalid"}
	_, err := resolveWatchRepos(args, nil)
	if err == nil {
		t.Error("expected error for invalid repo arg")
	}
}

func TestResolveWatchRepos_NoArgsUsesConfig(t *testing.T) {
	cfgRepos := []string{"org/repo1", "org/repo2"}
	repos, err := resolveWatchRepos(nil, cfgRepos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0] != "org/repo1" || repos[1] != "org/repo2" {
		t.Errorf("unexpected repos: %v", repos)
	}
}

func TestResolveWatchRepos_NoArgsNoConfig(t *testing.T) {
	_, err := resolveWatchRepos(nil, nil)
	if err == nil {
		t.Error("expected error when no repos provided and no config")
	}

	_, err = resolveWatchRepos([]string{}, []string{})
	if err == nil {
		t.Error("expected error when empty args and empty config")
	}
}

func TestResolveWatchRepos_ArgsOverrideConfig(t *testing.T) {
	args := []string{"org/specific"}
	cfgRepos := []string{"org/from-config"}
	repos, err := resolveWatchRepos(args, cfgRepos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0] != "org/specific" {
		t.Errorf("expected org/specific, got %s", repos[0])
	}
}

func TestMergeRepoLabels_Deduplicates(t *testing.T) {
	cfg := &config.Config{
		Repos: []config.RepoConfig{
			{
				Name: "org/repo1",
				Labels: []config.LabelConfig{
					{Name: "bug", Description: "Bug report"},
					{Name: "feature", Description: "Feature request"},
				},
			},
			{
				Name: "org/repo2",
				Labels: []config.LabelConfig{
					{Name: "bug", Description: "Bug report"},
					{Name: "docs", Description: "Documentation"},
				},
			},
		},
	}

	repos := []string{"org/repo1", "org/repo2"}
	labels := mergeRepoLabels(cfg, repos)

	// Should have 3 unique labels: bug, feature, docs
	if len(labels) != 3 {
		t.Fatalf("expected 3 unique labels, got %d: %v", len(labels), labels)
	}

	names := make(map[string]bool)
	for _, l := range labels {
		names[l.Name] = true
	}

	for _, expected := range []string{"bug", "feature", "docs"} {
		if !names[expected] {
			t.Errorf("expected label %q not found", expected)
		}
	}
}

func TestMergeRepoLabels_DefaultsWhenNoConfig(t *testing.T) {
	cfg := &config.Config{}
	repos := []string{"org/repo1"}
	labels := mergeRepoLabels(cfg, repos)

	// Should fall back to default labels from findRepoLabels
	if len(labels) == 0 {
		t.Error("expected default labels, got none")
	}
}
