package provider

import (
	"testing"
)

func TestNewOpenAIEmbedder_DefaultModel(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-api-key", "text-embedding-3-small")
	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
	if embedder.client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewOpenAIEmbedder_LargeModel(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-api-key", "text-embedding-3-large")
	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
	if embedder.client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewOpenAIEmbedder_UnknownModelDefaultsToSmall(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-api-key", "unknown-model")
	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
	// Should default to small model without error
}

func TestOpenAIEmbedder_ImplementsInterface(t *testing.T) {
	var _ Embedder = (*OpenAIEmbedder)(nil)
}

func TestOllamaEmbedder_ImplementsInterface(t *testing.T) {
	var _ Embedder = (*OllamaEmbedder)(nil)
}

func TestNewOllamaEmbedder(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
	if embedder.url != "http://localhost:11434" {
		t.Errorf("expected url http://localhost:11434, got %s", embedder.url)
	}
	if embedder.model != "nomic-embed-text" {
		t.Errorf("expected model nomic-embed-text, got %s", embedder.model)
	}
	if embedder.client == nil {
		t.Fatal("expected non-nil http client")
	}
}

func TestNewOllamaEmbedder_StripsTrailingSlash(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434/", "nomic-embed-text")
	if embedder.url != "http://localhost:11434" {
		t.Errorf("expected trailing slash stripped, got %s", embedder.url)
	}
}
