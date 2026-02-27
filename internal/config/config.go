package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	GitHub    GitHubConfig    `yaml:"github"`
	Providers ProvidersConfig `yaml:"providers"`
	Notify    NotifyConfig    `yaml:"notify"`
	Defaults  DefaultsConfig  `yaml:"defaults"`
	Store     StoreConfig     `yaml:"store"`
	Repos     []RepoConfig    `yaml:"repos"`
}

// GitHubConfig holds GitHub authentication settings.
type GitHubConfig struct {
	Auth           string `yaml:"auth"`
	AppID          string `yaml:"app_id"`
	InstallationID string `yaml:"installation_id"`
	PrivateKeyPath string `yaml:"private_key_path"`
	PrivateKey     string `yaml:"private_key"`
}

// ProviderConfig holds settings for a single provider (embedding or LLM).
type ProviderConfig struct {
	Type   string `yaml:"type"`
	Model  string `yaml:"model"`
	APIKey string `yaml:"api_key"`
	URL    string `yaml:"url"`
}

// ProvidersConfig groups embedding and LLM provider configs.
type ProvidersConfig struct {
	Embedding ProviderConfig `yaml:"embedding"`
	LLM       ProviderConfig `yaml:"llm"`
}

// NotifyConfig holds notification webhook URLs.
type NotifyConfig struct {
	SlackWebhook   string `yaml:"slack_webhook"`
	DiscordWebhook string `yaml:"discord_webhook"`
}

// DefaultsConfig holds default operational parameters.
type DefaultsConfig struct {
	PollIntervalRaw     string  `yaml:"poll_interval"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
	MaxDuplicatesShown  int     `yaml:"max_duplicates_shown"`
	EmbedMaxTokens      int     `yaml:"embed_max_tokens"`
	RequestTimeoutRaw   string  `yaml:"request_timeout"`
}

// StoreConfig holds storage settings.
type StoreConfig struct {
	Path string `yaml:"path"`
}

// LabelConfig defines a label with a description.
type LabelConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// RepoConfig holds per-repository overrides.
type RepoConfig struct {
	Name                string        `yaml:"name"`
	Labels              []LabelConfig `yaml:"labels"`
	CustomPrompt        string        `yaml:"custom_prompt"`
	SimilarityThreshold *float64      `yaml:"similarity_threshold"`
}

// PollInterval returns the parsed poll interval duration.
func (d DefaultsConfig) PollInterval() (time.Duration, error) {
	if d.PollIntervalRaw == "" {
		return 5 * time.Minute, nil
	}
	return time.ParseDuration(d.PollIntervalRaw)
}

// RequestTimeout returns the parsed request timeout duration.
func (d DefaultsConfig) RequestTimeout() (time.Duration, error) {
	if d.RequestTimeoutRaw == "" {
		return 30 * time.Second, nil
	}
	return time.ParseDuration(d.RequestTimeoutRaw)
}

// envVarPattern matches ${VAR} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// expandEnvVars replaces ${VAR} placeholders with environment variable values.
// Returns an error if any referenced variable is not set.
func expandEnvVars(data []byte) ([]byte, error) {
	var missing []string

	result := envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := envVarPattern.FindSubmatch(match)[1]
		val, ok := os.LookupEnv(string(varName))
		if !ok {
			missing = append(missing, string(varName))
			return match
		}
		return []byte(val)
	})

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return result, nil
}

// Load reads and parses a config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse parses config from raw YAML bytes, expanding env vars and validating.
func Parse(data []byte) (*Config, error) {
	expanded, err := expandEnvVars(data)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Defaults.PollIntervalRaw == "" {
		cfg.Defaults.PollIntervalRaw = "5m"
	}
	if cfg.Defaults.SimilarityThreshold == 0 {
		cfg.Defaults.SimilarityThreshold = 0.85
	}
	if cfg.Defaults.ConfidenceThreshold == 0 {
		cfg.Defaults.ConfidenceThreshold = 0.7
	}
	if cfg.Defaults.MaxDuplicatesShown == 0 {
		cfg.Defaults.MaxDuplicatesShown = 3
	}
	if cfg.Defaults.EmbedMaxTokens == 0 {
		cfg.Defaults.EmbedMaxTokens = 8192
	}
	if cfg.Defaults.RequestTimeoutRaw == "" {
		cfg.Defaults.RequestTimeoutRaw = "30s"
	}
	if cfg.Store.Path == "" {
		cfg.Store.Path = "~/.triage/triage.db"
	}
}

func validate(cfg *Config) error {
	// Validate thresholds are in [0, 1]
	if cfg.Defaults.SimilarityThreshold < 0 || cfg.Defaults.SimilarityThreshold > 1 {
		return fmt.Errorf("similarity_threshold must be between 0 and 1, got %f", cfg.Defaults.SimilarityThreshold)
	}
	if cfg.Defaults.ConfidenceThreshold < 0 || cfg.Defaults.ConfidenceThreshold > 1 {
		return fmt.Errorf("confidence_threshold must be between 0 and 1, got %f", cfg.Defaults.ConfidenceThreshold)
	}

	// Validate durations parse correctly
	if _, err := time.ParseDuration(cfg.Defaults.PollIntervalRaw); err != nil {
		return fmt.Errorf("invalid poll_interval %q: %w", cfg.Defaults.PollIntervalRaw, err)
	}
	if _, err := time.ParseDuration(cfg.Defaults.RequestTimeoutRaw); err != nil {
		return fmt.Errorf("invalid request_timeout %q: %w", cfg.Defaults.RequestTimeoutRaw, err)
	}

	// Validate per-repo similarity thresholds
	for _, repo := range cfg.Repos {
		if repo.SimilarityThreshold != nil {
			if *repo.SimilarityThreshold < 0 || *repo.SimilarityThreshold > 1 {
				return fmt.Errorf("repo %s: similarity_threshold must be between 0 and 1, got %f",
					repo.Name, *repo.SimilarityThreshold)
			}
		}
	}

	// Validate provider types if set
	validEmbedTypes := map[string]bool{"openai": true, "ollama": true, "": true}
	if !validEmbedTypes[cfg.Providers.Embedding.Type] {
		return fmt.Errorf("unsupported embedding provider type: %s", cfg.Providers.Embedding.Type)
	}

	validLLMTypes := map[string]bool{"openai": true, "ollama": true, "anthropic": true, "": true}
	if !validLLMTypes[cfg.Providers.LLM.Type] {
		return fmt.Errorf("unsupported LLM provider type: %s", cfg.Providers.LLM.Type)
	}

	return nil
}
