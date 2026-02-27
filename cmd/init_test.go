package cmd

import (
	"strings"
	"testing"
)

func TestBuildConfigYAML_OpenAI(t *testing.T) {
	result := buildConfigYAML("", "", "openai", "openai", "", "")

	if !strings.Contains(result, "model: text-embedding-3-small") {
		t.Error("expected OpenAI embedding model 'text-embedding-3-small' in config")
	}
	if !strings.Contains(result, "model: gpt-4o-mini") {
		t.Error("expected OpenAI LLM model 'gpt-4o-mini' in config")
	}
	// Check api_key appears for both providers
	if strings.Count(result, "${OPENAI_API_KEY}") != 2 {
		t.Errorf("expected two occurrences of ${OPENAI_API_KEY}, got %d", strings.Count(result, "${OPENAI_API_KEY}"))
	}
}

func TestBuildConfigYAML_Anthropic(t *testing.T) {
	result := buildConfigYAML("", "", "openai", "anthropic", "", "")

	if !strings.Contains(result, "type: anthropic") {
		t.Error("expected 'type: anthropic' in config")
	}
	if !strings.Contains(result, "model: claude-sonnet-4-20250514") {
		t.Errorf("expected Anthropic model 'claude-sonnet-4-20250514' in config, got:\n%s", result)
	}
	if !strings.Contains(result, "${ANTHROPIC_API_KEY}") {
		t.Errorf("expected ${ANTHROPIC_API_KEY} in config, got:\n%s", result)
	}
	// Embedding should still use OpenAI defaults
	if !strings.Contains(result, "model: text-embedding-3-small") {
		t.Error("expected OpenAI embedding model for embedding provider")
	}
}

func TestBuildConfigYAML_Ollama(t *testing.T) {
	result := buildConfigYAML("", "", "ollama", "ollama", "", "")

	if !strings.Contains(result, "model: nomic-embed-text") {
		t.Errorf("expected Ollama embedding model 'nomic-embed-text' in config, got:\n%s", result)
	}
	if !strings.Contains(result, "model: llama3") {
		t.Errorf("expected Ollama LLM model 'llama3' in config, got:\n%s", result)
	}
	// Ollama should not reference OpenAI/Anthropic API keys
	if strings.Contains(result, "${OPENAI_API_KEY}") {
		t.Error("Ollama config should not reference ${OPENAI_API_KEY}")
	}
	if strings.Contains(result, "${ANTHROPIC_API_KEY}") {
		t.Error("Ollama config should not reference ${ANTHROPIC_API_KEY}")
	}
}

func TestBuildConfigYAML_WithGitHub(t *testing.T) {
	result := buildConfigYAML("12345", "/path/to/key.pem", "openai", "openai", "", "")

	if !strings.Contains(result, "app_id: 12345") {
		t.Error("expected app_id in config")
	}
	if !strings.Contains(result, "private_key_path: /path/to/key.pem") {
		t.Error("expected private_key_path in config")
	}
}

func TestBuildConfigYAML_WithWebhooks(t *testing.T) {
	result := buildConfigYAML("", "", "openai", "openai", "https://hooks.slack.com/test", "https://discord.com/api/webhooks/test")

	if !strings.Contains(result, "slack_webhook: https://hooks.slack.com/test") {
		t.Error("expected slack_webhook in config")
	}
	if !strings.Contains(result, "discord_webhook: https://discord.com/api/webhooks/test") {
		t.Error("expected discord_webhook in config")
	}
}

func TestEmbeddingProviderDefaults(t *testing.T) {
	tests := []struct {
		provider      string
		expectedModel string
		expectedKey   string
	}{
		{"openai", "text-embedding-3-small", "${OPENAI_API_KEY}"},
		{"ollama", "nomic-embed-text", "# not required for ollama"},
		{"unknown", "text-embedding-3-small", "${OPENAI_API_KEY}"}, // falls back to openai
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			model, key := embeddingProviderDefaults(tc.provider)
			if model != tc.expectedModel {
				t.Errorf("embeddingProviderDefaults(%q) model = %q, want %q", tc.provider, model, tc.expectedModel)
			}
			if key != tc.expectedKey {
				t.Errorf("embeddingProviderDefaults(%q) key = %q, want %q", tc.provider, key, tc.expectedKey)
			}
		})
	}
}

func TestLLMProviderDefaults(t *testing.T) {
	tests := []struct {
		provider      string
		expectedModel string
		expectedKey   string
	}{
		{"openai", "gpt-4o-mini", "${OPENAI_API_KEY}"},
		{"anthropic", "claude-sonnet-4-20250514", "${ANTHROPIC_API_KEY}"},
		{"ollama", "llama3", "# not required for ollama"},
		{"unknown", "gpt-4o-mini", "${OPENAI_API_KEY}"}, // falls back to openai
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			model, key := llmProviderDefaults(tc.provider)
			if model != tc.expectedModel {
				t.Errorf("llmProviderDefaults(%q) model = %q, want %q", tc.provider, model, tc.expectedModel)
			}
			if key != tc.expectedKey {
				t.Errorf("llmProviderDefaults(%q) key = %q, want %q", tc.provider, key, tc.expectedKey)
			}
		})
	}
}
