package cmd

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/notify"
)

func TestCreateNotifier(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		notifyFlag string
		wantNil    bool
		wantErr    bool
	}{
		{
			name: "slack only from config",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					SlackWebhook: "https://hooks.slack.com/services/xxx",
				},
			},
			notifyFlag: "",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "discord only from config",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					DiscordWebhook: "https://discord.com/api/webhooks/xxx",
				},
			},
			notifyFlag: "",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "both from config",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					SlackWebhook:   "https://hooks.slack.com/services/xxx",
					DiscordWebhook: "https://discord.com/api/webhooks/xxx",
				},
			},
			notifyFlag: "",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "neither configured",
			cfg: &config.Config{
				Notify: config.NotifyConfig{},
			},
			notifyFlag: "",
			wantNil:    true,
			wantErr:    false,
		},
		{
			name: "flag override to slack",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					SlackWebhook: "https://hooks.slack.com/services/xxx",
				},
			},
			notifyFlag: "slack",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "flag override to discord",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					DiscordWebhook: "https://discord.com/api/webhooks/xxx",
				},
			},
			notifyFlag: "discord",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "flag override to both",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					SlackWebhook:   "https://hooks.slack.com/services/xxx",
					DiscordWebhook: "https://discord.com/api/webhooks/xxx",
				},
			},
			notifyFlag: "both",
			wantNil:    false,
			wantErr:    false,
		},
		{
			name: "flag slack but no webhook URL",
			cfg: &config.Config{
				Notify: config.NotifyConfig{},
			},
			notifyFlag: "slack",
			wantNil:    false,
			wantErr:    true,
		},
		{
			name: "flag discord but no webhook URL",
			cfg: &config.Config{
				Notify: config.NotifyConfig{},
			},
			notifyFlag: "discord",
			wantNil:    false,
			wantErr:    true,
		},
		{
			name: "flag both but missing slack URL",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					DiscordWebhook: "https://discord.com/api/webhooks/xxx",
				},
			},
			notifyFlag: "both",
			wantNil:    false,
			wantErr:    true,
		},
		{
			name: "flag both but missing discord URL",
			cfg: &config.Config{
				Notify: config.NotifyConfig{
					SlackWebhook: "https://hooks.slack.com/services/xxx",
				},
			},
			notifyFlag: "both",
			wantNil:    false,
			wantErr:    true,
		},
		{
			name: "unsupported notifier flag",
			cfg: &config.Config{
				Notify: config.NotifyConfig{},
			},
			notifyFlag: "email",
			wantNil:    false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := createNotifier(tt.cfg, tt.notifyFlag)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && n != nil {
				t.Error("expected nil notifier, got non-nil")
			}
			if !tt.wantNil && n == nil {
				t.Error("expected non-nil notifier, got nil")
			}
		})
	}
}

func TestCreateNotifierTypes(t *testing.T) {
	t.Run("slack returns SlackNotifier", func(t *testing.T) {
		cfg := &config.Config{
			Notify: config.NotifyConfig{
				SlackWebhook: "https://hooks.slack.com/services/xxx",
			},
		}
		n, err := createNotifier(cfg, "slack")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := n.(*notify.SlackNotifier); !ok {
			t.Errorf("expected *notify.SlackNotifier, got %T", n)
		}
	})

	t.Run("discord returns DiscordNotifier", func(t *testing.T) {
		cfg := &config.Config{
			Notify: config.NotifyConfig{
				DiscordWebhook: "https://discord.com/api/webhooks/xxx",
			},
		}
		n, err := createNotifier(cfg, "discord")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := n.(*notify.DiscordNotifier); !ok {
			t.Errorf("expected *notify.DiscordNotifier, got %T", n)
		}
	})

	t.Run("both returns MultiNotifier", func(t *testing.T) {
		cfg := &config.Config{
			Notify: config.NotifyConfig{
				SlackWebhook:   "https://hooks.slack.com/services/xxx",
				DiscordWebhook: "https://discord.com/api/webhooks/xxx",
			},
		}
		n, err := createNotifier(cfg, "both")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := n.(*notify.MultiNotifier); !ok {
			t.Errorf("expected *notify.MultiNotifier, got %T", n)
		}
	})
}

func TestFindRepoLabels(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		fullName string
		wantLen  int
		wantName string // first label name to check
	}{
		{
			name: "matching repo with custom labels",
			cfg: &config.Config{
				Repos: []config.RepoConfig{
					{
						Name: "owner/repo",
						Labels: []config.LabelConfig{
							{Name: "critical", Description: "Critical issue"},
							{Name: "minor", Description: "Minor issue"},
						},
					},
				},
			},
			fullName: "owner/repo",
			wantLen:  2,
			wantName: "critical",
		},
		{
			name: "matching repo with empty labels falls back to defaults",
			cfg: &config.Config{
				Repos: []config.RepoConfig{
					{
						Name:   "owner/repo",
						Labels: []config.LabelConfig{},
					},
				},
			},
			fullName: "owner/repo",
			wantLen:  5,
			wantName: "bug",
		},
		{
			name: "no matching repo falls back to defaults",
			cfg: &config.Config{
				Repos: []config.RepoConfig{
					{
						Name: "other/repo",
						Labels: []config.LabelConfig{
							{Name: "critical", Description: "Critical issue"},
						},
					},
				},
			},
			fullName: "owner/repo",
			wantLen:  5,
			wantName: "bug",
		},
		{
			name:     "empty repos config falls back to defaults",
			cfg:      &config.Config{},
			fullName: "owner/repo",
			wantLen:  5,
			wantName: "bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := findRepoLabels(tt.cfg, tt.fullName)
			if len(labels) != tt.wantLen {
				t.Errorf("expected %d labels, got %d", tt.wantLen, len(labels))
			}
			if len(labels) > 0 && labels[0].Name != tt.wantName {
				t.Errorf("expected first label %q, got %q", tt.wantName, labels[0].Name)
			}
		})
	}
}

func TestFindRepoLabelsDefaults(t *testing.T) {
	cfg := &config.Config{}
	labels := findRepoLabels(cfg, "any/repo")

	expectedNames := []string{"bug", "feature", "question", "documentation", "enhancement"}
	if len(labels) != len(expectedNames) {
		t.Fatalf("expected %d default labels, got %d", len(expectedNames), len(labels))
	}
	for i, expected := range expectedNames {
		if labels[i].Name != expected {
			t.Errorf("default label[%d]: expected %q, got %q", i, expected, labels[i].Name)
		}
		if labels[i].Description == "" {
			t.Errorf("default label[%d] %q: expected non-empty description", i, expected)
		}
	}
}

func TestInitComponentsWithMemoryStore(t *testing.T) {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: ":memory:",
		},
		Providers: config.ProvidersConfig{
			Embedding: config.ProviderConfig{
				Type:   "openai",
				APIKey: "test-key",
				Model:  "text-embedding-3-small",
			},
			LLM: config.ProviderConfig{
				Type:   "openai",
				APIKey: "test-key",
				Model:  "gpt-4o-mini",
			},
		},
		Defaults: config.DefaultsConfig{
			SimilarityThreshold: 0.85,
			MaxDuplicatesShown:  3,
			RequestTimeoutRaw:   "30s",
		},
	}

	logger := slog.Default()
	c, err := initComponents(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Store.Close()

	if c.Store == nil {
		t.Error("expected Store to be non-nil")
	}
	if c.Embedder == nil {
		t.Error("expected Embedder to be non-nil for openai provider")
	}
	if c.Completer == nil {
		t.Error("expected Completer to be non-nil for openai provider")
	}
	if c.Dedup == nil {
		t.Error("expected Dedup to be non-nil when embedder is set")
	}
	if c.Classifier == nil {
		t.Error("expected Classifier to be non-nil when completer is set")
	}
	if c.Broker == nil {
		t.Error("expected Broker to be non-nil")
	}
	if c.Config != cfg {
		t.Error("expected Config to match input")
	}
	if c.Logger != logger {
		t.Error("expected Logger to match input")
	}
}

func TestInitComponentsWithOllamaProviders(t *testing.T) {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: ":memory:",
		},
		Providers: config.ProvidersConfig{
			Embedding: config.ProviderConfig{
				Type:  "ollama",
				URL:   "http://localhost:11434",
				Model: "nomic-embed-text",
			},
			LLM: config.ProviderConfig{
				Type:  "ollama",
				URL:   "http://localhost:11434",
				Model: "llama3",
			},
		},
		Defaults: config.DefaultsConfig{
			SimilarityThreshold: 0.85,
			MaxDuplicatesShown:  3,
			RequestTimeoutRaw:   "30s",
		},
	}

	logger := slog.Default()
	c, err := initComponents(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Store.Close()

	if c.Embedder == nil {
		t.Error("expected Embedder to be non-nil for ollama provider")
	}
	if c.Completer == nil {
		t.Error("expected Completer to be non-nil for ollama provider")
	}
	if c.Dedup == nil {
		t.Error("expected Dedup to be non-nil when embedder is set")
	}
	if c.Classifier == nil {
		t.Error("expected Classifier to be non-nil when completer is set")
	}
}

func TestInitComponentsWithAnthropicLLM(t *testing.T) {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: ":memory:",
		},
		Providers: config.ProvidersConfig{
			Embedding: config.ProviderConfig{
				Type:   "openai",
				APIKey: "test-key",
				Model:  "text-embedding-3-small",
			},
			LLM: config.ProviderConfig{
				Type:   "anthropic",
				APIKey: "test-key",
				Model:  "claude-3-haiku",
			},
		},
		Defaults: config.DefaultsConfig{
			SimilarityThreshold: 0.85,
			MaxDuplicatesShown:  3,
			RequestTimeoutRaw:   "30s",
		},
	}

	logger := slog.Default()
	c, err := initComponents(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Store.Close()

	if c.Completer == nil {
		t.Error("expected Completer to be non-nil for anthropic provider")
	}
}

func TestInitComponentsNoProviders(t *testing.T) {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: ":memory:",
		},
		Providers: config.ProvidersConfig{},
		Defaults: config.DefaultsConfig{
			SimilarityThreshold: 0.85,
			MaxDuplicatesShown:  3,
			RequestTimeoutRaw:   "30s",
		},
	}

	logger := slog.Default()
	c, err := initComponents(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Store.Close()

	if c.Embedder != nil {
		t.Error("expected Embedder to be nil with no embedding provider")
	}
	if c.Completer != nil {
		t.Error("expected Completer to be nil with no LLM provider")
	}
	if c.Dedup != nil {
		t.Error("expected Dedup to be nil when embedder is nil")
	}
	if c.Classifier != nil {
		t.Error("expected Classifier to be nil when completer is nil")
	}
	// Broker should still be created
	if c.Broker == nil {
		t.Error("expected Broker to be non-nil even without providers")
	}
}

func TestInitComponentsInvalidStorePath(t *testing.T) {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: "/nonexistent/deeply/nested/path/triage.db",
		},
		Defaults: config.DefaultsConfig{
			RequestTimeoutRaw: "30s",
		},
	}

	logger := slog.Default()
	_, err := initComponents(cfg, logger)
	if err == nil {
		t.Error("expected error for invalid store path, got nil")
	}
}

func TestInitComponentsRequestTimeoutFallback(t *testing.T) {
	// When RequestTimeoutRaw is invalid, initComponents should fall back to 30s
	cfg := &config.Config{
		Store: config.StoreConfig{
			Path: ":memory:",
		},
		Providers: config.ProvidersConfig{
			LLM: config.ProviderConfig{
				Type:   "openai",
				APIKey: "test-key",
				Model:  "gpt-4o-mini",
			},
		},
		Defaults: config.DefaultsConfig{
			SimilarityThreshold: 0.85,
			MaxDuplicatesShown:  3,
			RequestTimeoutRaw:   "invalid-duration",
		},
	}

	logger := slog.Default()
	c, err := initComponents(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Store.Close()

	// Classifier should still be created with the fallback timeout
	if c.Classifier == nil {
		t.Error("expected Classifier to be non-nil with fallback timeout")
	}
}

func TestParseIssueRef(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{
			name:       "valid reference",
			ref:        "owner/repo#42",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 42,
		},
		{
			name:       "valid with hyphens and dots",
			ref:        "my-org/my.repo#123",
			wantOwner:  "my-org",
			wantRepo:   "my.repo",
			wantNumber: 123,
		},
		{
			name:    "missing hash",
			ref:     "owner/repo",
			wantErr: true,
		},
		{
			name:    "missing repo",
			ref:     "owner#42",
			wantErr: true,
		},
		{
			name:    "invalid number",
			ref:     "owner/repo#abc",
			wantErr: true,
		},
		{
			name:    "empty string",
			ref:     "",
			wantErr: true,
		},
		{
			name:       "number with hash in repo name",
			ref:        "owner/repo-with#hash#99",
			wantOwner:  "owner",
			wantRepo:   "repo-with#hash",
			wantNumber: 99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parseIssueRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner: expected %q, got %q", tt.wantOwner, owner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo: expected %q, got %q", tt.wantRepo, repo)
			}
			if number != tt.wantNumber {
				t.Errorf("number: expected %d, got %d", tt.wantNumber, number)
			}
		})
	}
}

func TestBuildConfigYAML(t *testing.T) {
	tests := []struct {
		name          string
		appID         string
		keyPath       string
		embedProvider string
		llmProvider   string
		slackURL      string
		discordURL    string
		wantContains  []string
		wantExcludes  []string
	}{
		{
			name:          "all fields populated",
			appID:         "12345",
			keyPath:       "/path/to/key.pem",
			embedProvider: "openai",
			llmProvider:   "anthropic",
			slackURL:      "https://hooks.slack.com/xxx",
			discordURL:    "https://discord.com/api/webhooks/xxx",
			wantContains: []string{
				"app_id: 12345",
				"private_key_path: /path/to/key.pem",
				"type: openai",
				"type: anthropic",
				"slack_webhook: https://hooks.slack.com/xxx",
				"discord_webhook: https://discord.com/api/webhooks/xxx",
			},
		},
		{
			name:          "empty fields use comments",
			appID:         "",
			keyPath:       "",
			embedProvider: "ollama",
			llmProvider:   "ollama",
			slackURL:      "",
			discordURL:    "",
			wantContains: []string{
				"# app_id: YOUR_APP_ID",
				"# private_key_path:",
				"type: ollama",
				"# slack_webhook:",
				"# discord_webhook:",
			},
		},
		{
			name:          "default store path present",
			appID:         "",
			keyPath:       "",
			embedProvider: "openai",
			llmProvider:   "openai",
			slackURL:      "",
			discordURL:    "",
			wantContains: []string{
				"path: ~/.triage/triage.db",
				"poll_interval: 5m",
				"similarity_threshold: 0.85",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildConfigYAML(tt.appID, tt.keyPath, tt.embedProvider, tt.llmProvider, tt.slackURL, tt.discordURL)
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("expected config to contain %q, but it did not.\nConfig:\n%s", want, result)
				}
			}
			for _, exclude := range tt.wantExcludes {
				if strings.Contains(result, exclude) {
					t.Errorf("expected config NOT to contain %q, but it did.\nConfig:\n%s", exclude, result)
				}
			}
		})
	}
}
