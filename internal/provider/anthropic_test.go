package provider

import "testing"

func TestNewAnthropicCompleter_DefaultModel(t *testing.T) {
	c := NewAnthropicCompleter("test-key", "")
	if c.model != defaultAnthropicModel {
		t.Errorf("expected default model %q, got %q", defaultAnthropicModel, c.model)
	}
	if c.client == nil {
		t.Error("client should not be nil")
	}
}

func TestNewAnthropicCompleter_CustomModel(t *testing.T) {
	c := NewAnthropicCompleter("test-key", "claude-3-haiku-20240307")
	if c.model != "claude-3-haiku-20240307" {
		t.Errorf("expected custom model, got %q", c.model)
	}
}

func TestNewOpenAICompleter_DefaultModel(t *testing.T) {
	c := NewOpenAICompleter("test-key", "")
	if c.model != defaultOpenAIModel {
		t.Errorf("expected default model %q, got %q", defaultOpenAIModel, c.model)
	}
	if c.client == nil {
		t.Error("client should not be nil")
	}
}

func TestNewOpenAICompleter_CustomModel(t *testing.T) {
	c := NewOpenAICompleter("test-key", "gpt-4")
	if c.model != "gpt-4" {
		t.Errorf("expected custom model, got %q", c.model)
	}
}

func TestNewOllamaCompleter_Defaults(t *testing.T) {
	c := NewOllamaCompleter("", "")
	if c.url != defaultOllamaURL {
		t.Errorf("expected default URL %q, got %q", defaultOllamaURL, c.url)
	}
	if c.model != defaultOllamaModel {
		t.Errorf("expected default model %q, got %q", defaultOllamaModel, c.model)
	}
	if c.client == nil {
		t.Error("client should not be nil")
	}
}

func TestNewOllamaCompleter_Custom(t *testing.T) {
	c := NewOllamaCompleter("http://custom:1234", "mistral")
	if c.url != "http://custom:1234" {
		t.Errorf("expected custom URL, got %q", c.url)
	}
	if c.model != "mistral" {
		t.Errorf("expected custom model, got %q", c.model)
	}
}
