package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/jacklau/triage/internal/classify"
	"github.com/jacklau/triage/internal/config"
	"github.com/jacklau/triage/internal/dedup"
	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/notify"
	"github.com/jacklau/triage/internal/pipeline"
	"github.com/jacklau/triage/internal/provider"
	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/store"

	gogithub "github.com/google/go-github/v60/github"
)

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "triage",
	Short: "Watch GitHub repos for new issues and triage them with AI",
	Long: `Triage watches GitHub repositories for new issues, detects duplicates
via AI embeddings, classifies them with LLMs, and sends results to
Slack/Discord for human review.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file (default %s)", defaultConfigPath()))
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".triage/config.yaml"
	}
	return home + "/.triage/config.yaml"
}

func setupLogger() *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(handler)
}

func loadConfig() (*config.Config, error) {
	path := cfgFile
	if path == "" {
		path = defaultConfigPath()
	}
	return config.Load(path)
}

// components holds initialized components for use by subcommands.
type components struct {
	Config     *config.Config
	Store      *store.DB
	GHClient   *gogithub.Client
	Embedder   provider.Embedder
	Completer  provider.Completer
	Dedup      *dedup.Engine
	Classifier *classify.Classifier
	Broker     *pubsub.Broker[github.IssueEvent]
	Logger     *slog.Logger
}

// initComponents creates all components from config.
func initComponents(cfg *config.Config, logger *slog.Logger) (*components, error) {
	c := &components{
		Config: cfg,
		Logger: logger,
	}

	// Open store
	db, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	c.Store = db

	// Create GitHub client
	if cfg.GitHub.Auth == "app" {
		appID, err := strconv.ParseInt(cfg.GitHub.AppID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing app_id: %w", err)
		}
		installID, err := strconv.ParseInt(cfg.GitHub.InstallationID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing installation_id: %w", err)
		}
		client, err := github.NewGitHubClient(appID, installID, []byte(cfg.GitHub.PrivateKey), cfg.GitHub.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("creating GitHub client: %w", err)
		}
		c.GHClient = client
	}

	// Create embedding provider
	switch cfg.Providers.Embedding.Type {
	case "openai":
		c.Embedder = provider.NewOpenAIEmbedder(cfg.Providers.Embedding.APIKey, cfg.Providers.Embedding.Model)
	case "ollama":
		c.Embedder = provider.NewOllamaEmbedder(cfg.Providers.Embedding.URL, cfg.Providers.Embedding.Model)
	case "":
		// No embedding provider configured
	default:
		return nil, fmt.Errorf("unsupported embedding provider type: %q", cfg.Providers.Embedding.Type)
	}

	// Create LLM provider
	switch cfg.Providers.LLM.Type {
	case "openai":
		c.Completer = provider.NewOpenAICompleter(cfg.Providers.LLM.APIKey, cfg.Providers.LLM.Model)
	case "anthropic":
		c.Completer = provider.NewAnthropicCompleter(cfg.Providers.LLM.APIKey, cfg.Providers.LLM.Model)
	case "ollama":
		c.Completer = provider.NewOllamaCompleter(cfg.Providers.LLM.URL, cfg.Providers.LLM.Model)
	case "":
		// No LLM provider configured
	default:
		return nil, fmt.Errorf("unsupported LLM provider type: %q", cfg.Providers.LLM.Type)
	}

	// Create dedup engine
	if c.Embedder != nil {
		opts := []dedup.Option{
			dedup.WithThreshold(float32(cfg.Defaults.SimilarityThreshold)),
			dedup.WithMaxCandidates(cfg.Defaults.MaxDuplicatesShown),
		}
		c.Dedup = dedup.NewEngine(c.Embedder, db, opts...)
	}

	// Create classifier
	if c.Completer != nil {
		timeout, err := cfg.Defaults.RequestTimeout()
		if err != nil {
			timeout = 30 * time.Second
		}
		c.Classifier = classify.NewClassifier(c.Completer, timeout)
	}

	// Create broker
	c.Broker = pubsub.NewBroker[github.IssueEvent]()

	return c, nil
}

// createNotifier builds a Notifier from config and flag override.
func createNotifier(cfg *config.Config, notifyFlag string) (notify.Notifier, error) {
	notifyType := notifyFlag
	if notifyType == "" {
		// Determine from config
		hasSlack := cfg.Notify.SlackWebhook != ""
		hasDiscord := cfg.Notify.DiscordWebhook != ""
		switch {
		case hasSlack && hasDiscord:
			notifyType = "both"
		case hasSlack:
			notifyType = "slack"
		case hasDiscord:
			notifyType = "discord"
		default:
			return nil, nil // no notification configured
		}
	}

	return notify.NewNotifier(notifyType, cfg.Notify.SlackWebhook, cfg.Notify.DiscordWebhook)
}

// createPoller builds a Poller for the specified repo.
func createPoller(c *components, owner, repo string) *github.Poller {
	return github.NewPoller(c.GHClient, c.Store, c.Broker, owner, repo)
}

// createPipeline builds a Pipeline from components.
func createPipeline(c *components, n notify.Notifier, labels []config.LabelConfig) *pipeline.Pipeline {
	return pipeline.New(pipeline.PipelineDeps{
		Dedup:       c.Dedup,
		Classifier:  c.Classifier,
		Notifier:    n,
		Store:       c.Store,
		Broker:      c.Broker,
		Labels:      labels,
		RepoConfigs: c.Config.Repos,
		Logger:      c.Logger,
	})
}

// findRepoLabels looks up configured labels for a given owner/repo, falling back to defaults.
func findRepoLabels(cfg *config.Config, fullName string) []config.LabelConfig {
	for _, rc := range cfg.Repos {
		if rc.Name == fullName {
			if len(rc.Labels) > 0 {
				return rc.Labels
			}
		}
	}
	// Return a default set of labels
	return []config.LabelConfig{
		{Name: "bug", Description: "Something isn't working"},
		{Name: "feature", Description: "New feature or request"},
		{Name: "question", Description: "Further information is requested"},
		{Name: "documentation", Description: "Improvements or additions to documentation"},
		{Name: "enhancement", Description: "Improvement to an existing feature"},
	}
}
