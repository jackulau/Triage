package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup for Triage configuration",
	Long:  `Creates a default configuration file with guided prompts.`,
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to Triage setup!")
	fmt.Println("This will create a configuration file for you.")
	fmt.Println()

	configPath := cfgFile
	if configPath == "" {
		configPath = defaultConfigPath()
	}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config file already exists at %s\n", configPath)
		fmt.Print("Overwrite? [y/N]: ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Gather inputs
	fmt.Print("GitHub App ID (or press Enter to skip): ")
	appID, _ := reader.ReadString('\n')
	appID = strings.TrimSpace(appID)

	fmt.Print("GitHub private key path (or press Enter to skip): ")
	keyPath, _ := reader.ReadString('\n')
	keyPath = strings.TrimSpace(keyPath)

	fmt.Print("Embedding provider (openai/ollama) [openai]: ")
	embedProvider, _ := reader.ReadString('\n')
	embedProvider = strings.TrimSpace(embedProvider)
	if embedProvider == "" {
		embedProvider = "openai"
	}

	fmt.Print("LLM provider (openai/ollama/anthropic) [openai]: ")
	llmProvider, _ := reader.ReadString('\n')
	llmProvider = strings.TrimSpace(llmProvider)
	if llmProvider == "" {
		llmProvider = "openai"
	}

	fmt.Print("Slack webhook URL (or press Enter to skip): ")
	slackURL, _ := reader.ReadString('\n')
	slackURL = strings.TrimSpace(slackURL)

	fmt.Print("Discord webhook URL (or press Enter to skip): ")
	discordURL, _ := reader.ReadString('\n')
	discordURL = strings.TrimSpace(discordURL)

	// Build config
	config := buildConfigYAML(appID, keyPath, embedProvider, llmProvider, slackURL, discordURL)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	fmt.Printf("\nConfig written to %s\n", configPath)
	fmt.Println("Edit the file to add API keys and customize settings.")
	return nil
}

func buildConfigYAML(appID, keyPath, embedProvider, llmProvider, slackURL, discordURL string) string {
	var b strings.Builder

	b.WriteString("# Triage configuration\n")
	b.WriteString("# See documentation for all available options.\n\n")

	b.WriteString("github:\n")
	if appID != "" {
		b.WriteString(fmt.Sprintf("  app_id: %s\n", appID))
	} else {
		b.WriteString("  # app_id: YOUR_APP_ID\n")
	}
	if keyPath != "" {
		b.WriteString(fmt.Sprintf("  private_key_path: %s\n", keyPath))
	} else {
		b.WriteString("  # private_key_path: /path/to/private-key.pem\n")
	}
	b.WriteString("  # installation_id: YOUR_INSTALLATION_ID\n")
	b.WriteString("\n")

	b.WriteString("providers:\n")
	b.WriteString("  embedding:\n")
	b.WriteString(fmt.Sprintf("    type: %s\n", embedProvider))
	embedModel, embedAPIKey := embeddingProviderDefaults(embedProvider)
	b.WriteString(fmt.Sprintf("    model: %s\n", embedModel))
	b.WriteString(fmt.Sprintf("    api_key: %s\n", embedAPIKey))
	b.WriteString("  llm:\n")
	b.WriteString(fmt.Sprintf("    type: %s\n", llmProvider))
	llmModel, llmAPIKey := llmProviderDefaults(llmProvider)
	b.WriteString(fmt.Sprintf("    model: %s\n", llmModel))
	b.WriteString(fmt.Sprintf("    api_key: %s\n", llmAPIKey))
	b.WriteString("\n")

	b.WriteString("notify:\n")
	if slackURL != "" {
		b.WriteString(fmt.Sprintf("  slack_webhook: %s\n", slackURL))
	} else {
		b.WriteString("  # slack_webhook: https://hooks.slack.com/services/...\n")
	}
	if discordURL != "" {
		b.WriteString(fmt.Sprintf("  discord_webhook: %s\n", discordURL))
	} else {
		b.WriteString("  # discord_webhook: https://discord.com/api/webhooks/...\n")
	}
	b.WriteString("\n")

	b.WriteString("defaults:\n")
	b.WriteString("  poll_interval: 5m\n")
	b.WriteString("  similarity_threshold: 0.85\n")
	b.WriteString("  confidence_threshold: 0.7\n")
	b.WriteString("  max_duplicates_shown: 3\n")
	b.WriteString("  embed_max_tokens: 8192\n")
	b.WriteString("  request_timeout: 30s\n")
	b.WriteString("\n")

	b.WriteString("store:\n")
	b.WriteString("  path: ~/.triage/triage.db\n")

	return b.String()
}

// embeddingProviderDefaults returns the default model and api_key placeholder
// for the given embedding provider type.
func embeddingProviderDefaults(provider string) (model, apiKey string) {
	switch provider {
	case "ollama":
		return "nomic-embed-text", "# not required for ollama"
	default: // openai
		return "text-embedding-3-small", "${OPENAI_API_KEY}"
	}
}

// llmProviderDefaults returns the default model and api_key placeholder
// for the given LLM provider type.
func llmProviderDefaults(provider string) (model, apiKey string) {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-20250514", "${ANTHROPIC_API_KEY}"
	case "ollama":
		return "llama3", "# not required for ollama"
	default: // openai
		return "gpt-4o-mini", "${OPENAI_API_KEY}"
	}
}
