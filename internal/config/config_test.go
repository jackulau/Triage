package config

import (
	"os"
	"testing"
)

func TestParseBasicConfig(t *testing.T) {
	yaml := `
github:
  app_id: "12345"
  private_key_path: /path/to/key.pem
providers:
  embedding:
    type: openai
    model: text-embedding-3-small
    api_key: sk-test-key
  llm:
    type: openai
    model: gpt-4o-mini
    api_key: sk-test-key
notify:
  slack_webhook: https://hooks.slack.com/test
defaults:
  poll_interval: 10m
  similarity_threshold: 0.9
  confidence_threshold: 0.8
  max_duplicates_shown: 5
  embed_max_tokens: 4096
  request_timeout: 60s
store:
  path: /tmp/triage.db
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GitHub.AppID != "12345" {
		t.Errorf("expected app_id '12345', got %q", cfg.GitHub.AppID)
	}
	if cfg.Providers.Embedding.Type != "openai" {
		t.Errorf("expected embedding type 'openai', got %q", cfg.Providers.Embedding.Type)
	}
	if cfg.Providers.LLM.Model != "gpt-4o-mini" {
		t.Errorf("expected llm model 'gpt-4o-mini', got %q", cfg.Providers.LLM.Model)
	}
	if cfg.Notify.SlackWebhook != "https://hooks.slack.com/test" {
		t.Errorf("expected slack webhook, got %q", cfg.Notify.SlackWebhook)
	}
	if cfg.Defaults.SimilarityThreshold != 0.9 {
		t.Errorf("expected similarity 0.9, got %f", cfg.Defaults.SimilarityThreshold)
	}
	if cfg.Defaults.ConfidenceThreshold != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", cfg.Defaults.ConfidenceThreshold)
	}
	if cfg.Defaults.MaxDuplicatesShown != 5 {
		t.Errorf("expected max_duplicates 5, got %d", cfg.Defaults.MaxDuplicatesShown)
	}
	if cfg.Store.Path != "/tmp/triage.db" {
		t.Errorf("expected store path '/tmp/triage.db', got %q", cfg.Store.Path)
	}

	dur, err := cfg.Defaults.PollInterval()
	if err != nil {
		t.Fatalf("unexpected error parsing poll interval: %v", err)
	}
	if dur.Minutes() != 10 {
		t.Errorf("expected 10m poll interval, got %v", dur)
	}

	timeout, err := cfg.Defaults.RequestTimeout()
	if err != nil {
		t.Fatalf("unexpected error parsing request timeout: %v", err)
	}
	if timeout.Seconds() != 60 {
		t.Errorf("expected 60s timeout, got %v", timeout)
	}
}

func TestParseDefaults(t *testing.T) {
	yaml := `
github: {}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Defaults.PollIntervalRaw != "5m" {
		t.Errorf("expected default poll_interval '5m', got %q", cfg.Defaults.PollIntervalRaw)
	}
	if cfg.Defaults.SimilarityThreshold != 0.85 {
		t.Errorf("expected default similarity 0.85, got %f", cfg.Defaults.SimilarityThreshold)
	}
	if cfg.Defaults.ConfidenceThreshold != 0.7 {
		t.Errorf("expected default confidence 0.7, got %f", cfg.Defaults.ConfidenceThreshold)
	}
	if cfg.Defaults.MaxDuplicatesShown != 3 {
		t.Errorf("expected default max_duplicates 3, got %d", cfg.Defaults.MaxDuplicatesShown)
	}
	if cfg.Defaults.EmbedMaxTokens != 8192 {
		t.Errorf("expected default embed_max_tokens 8192, got %d", cfg.Defaults.EmbedMaxTokens)
	}
	if cfg.Defaults.RequestTimeoutRaw != "30s" {
		t.Errorf("expected default request_timeout '30s', got %q", cfg.Defaults.RequestTimeoutRaw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	expectedStorePath := home + "/.triage/triage.db"
	if cfg.Store.Path != expectedStorePath {
		t.Errorf("expected default store path %q, got %q", expectedStorePath, cfg.Store.Path)
	}
}

func TestEnvVarExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "my-secret-key")
	defer os.Unsetenv("TEST_API_KEY")

	yaml := `
providers:
  embedding:
    type: openai
    api_key: ${TEST_API_KEY}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Providers.Embedding.APIKey != "my-secret-key" {
		t.Errorf("expected api_key 'my-secret-key', got %q", cfg.Providers.Embedding.APIKey)
	}
}

func TestEnvVarMissing(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR_12345")

	yaml := `
providers:
  embedding:
    api_key: ${NONEXISTENT_VAR_12345}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing env var, got nil")
	}

	expected := "missing required environment variables: NONEXISTENT_VAR_12345"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestValidationInvalidThreshold(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "similarity too high",
			yaml: `
defaults:
  similarity_threshold: 1.5
`,
		},
		{
			name: "similarity negative",
			yaml: `
defaults:
  similarity_threshold: -0.1
`,
		},
		{
			name: "confidence too high",
			yaml: `
defaults:
  confidence_threshold: 2.0
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestValidationInvalidDuration(t *testing.T) {
	yaml := `
defaults:
  poll_interval: not-a-duration
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for invalid duration, got nil")
	}
}

func TestValidationInvalidProviderType(t *testing.T) {
	yaml := `
providers:
  embedding:
    type: invalid_provider
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for invalid provider type, got nil")
	}
}

func TestRepoConfig(t *testing.T) {
	threshold := 0.95
	yaml := `
repos:
  - name: myorg/myrepo
    labels:
      - name: bug
        description: Something is broken
      - name: feature
        description: New feature request
    custom_prompt: "Focus on security issues"
    similarity_threshold: 0.95
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Repos) != 1 {
		t.Fatalf("expected 1 repo config, got %d", len(cfg.Repos))
	}

	repo := cfg.Repos[0]
	if repo.Name != "myorg/myrepo" {
		t.Errorf("expected repo name 'myorg/myrepo', got %q", repo.Name)
	}
	if len(repo.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(repo.Labels))
	}
	if repo.CustomPrompt != "Focus on security issues" {
		t.Errorf("expected custom prompt, got %q", repo.CustomPrompt)
	}
	if repo.SimilarityThreshold == nil || *repo.SimilarityThreshold != threshold {
		t.Errorf("expected similarity threshold %f, got %v", threshold, repo.SimilarityThreshold)
	}
}

func TestRepoInvalidThreshold(t *testing.T) {
	yaml := `
repos:
  - name: myorg/myrepo
    similarity_threshold: 5.0
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for repo threshold, got nil")
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde prefix",
			input:    "~/.triage/triage.db",
			expected: home + "/.triage/triage.db",
		},
		{
			name:     "tilde only",
			input:    "~",
			expected: home,
		},
		{
			name:     "absolute path unchanged",
			input:    "/tmp/triage.db",
			expected: "/tmp/triage.db",
		},
		{
			name:     "relative path unchanged",
			input:    "data/triage.db",
			expected: "data/triage.db",
		},
		{
			name:     "tilde in middle unchanged",
			input:    "/some/~/path",
			expected: "/some/~/path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := expandTilde(tc.input)
			if result != tc.expected {
				t.Errorf("expandTilde(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestTildeExpansionInStorePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	yaml := `
store:
  path: "~/.triage/triage.db"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := home + "/.triage/triage.db"
	if cfg.Store.Path != expected {
		t.Errorf("expected store path %q, got %q", expected, cfg.Store.Path)
	}
}

func TestAbsoluteStorePathUnchanged(t *testing.T) {
	yaml := `
store:
  path: /var/data/triage.db
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Store.Path != "/var/data/triage.db" {
		t.Errorf("expected store path '/var/data/triage.db', got %q", cfg.Store.Path)
	}
}

func TestValidationInvalidLLMProviderType(t *testing.T) {
	yaml := `
providers:
  llm:
    type: openAI
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for invalid LLM provider type 'openAI', got nil")
	}
}

func TestValidationInvalidEmbeddingProviderType(t *testing.T) {
	yaml := `
providers:
  embedding:
    type: OpenAI
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for invalid embedding provider type 'OpenAI', got nil")
	}
}

func TestValidationValidProviderTypes(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "openai embedding and llm",
			yaml: `
providers:
  embedding:
    type: openai
    api_key: test
  llm:
    type: openai
    api_key: test
`,
		},
		{
			name: "ollama embedding and llm",
			yaml: `
providers:
  embedding:
    type: ollama
  llm:
    type: ollama
`,
		},
		{
			name: "anthropic llm",
			yaml: `
providers:
  llm:
    type: anthropic
    api_key: test
`,
		},
		{
			name: "empty provider types",
			yaml: `
github: {}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err != nil {
				t.Errorf("unexpected error for valid config: %v", err)
			}
		})
	}
}
