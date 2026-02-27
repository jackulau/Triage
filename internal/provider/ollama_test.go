package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaEmbedder_ClientTimeout(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
	if embedder.client.Timeout != 30*time.Second {
		t.Errorf("expected client timeout of 30s, got %v", embedder.client.Timeout)
	}
}

func TestOllamaCompleter_ClientTimeout(t *testing.T) {
	completer := NewOllamaCompleter("http://localhost:11434", "llama3.1:8b")
	if completer.client.Timeout != 30*time.Second {
		t.Errorf("expected client timeout of 30s, got %v", completer.client.Timeout)
	}
}

func TestOllamaCompleter_DefaultClientTimeout(t *testing.T) {
	completer := NewOllamaCompleter("", "")
	if completer.client.Timeout != 30*time.Second {
		t.Errorf("expected client timeout of 30s, got %v", completer.client.Timeout)
	}
	if completer.url != defaultOllamaURL {
		t.Errorf("expected default URL %q, got %q", defaultOllamaURL, completer.url)
	}
	if completer.model != defaultOllamaModel {
		t.Errorf("expected default model %q, got %q", defaultOllamaModel, completer.model)
	}
}

func TestOllamaEmbedder_TimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	embedder.client.Timeout = 100 * time.Millisecond

	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestOllamaCompleter_TimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	completer := NewOllamaCompleter(server.URL, "llama3.1:8b")
	completer.client.Timeout = 100 * time.Millisecond

	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestOllamaEmbedder_SuccessfulEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		resp := ollamaEmbeddingResponse{
			Embedding: []float64{0.1, 0.2, 0.3},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	result, err := embedder.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(result))
	}
}

func TestOllamaCompleter_SuccessfulComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected path /api/generate, got %s", r.URL.Path)
		}

		resp := ollamaCompletionResponse{
			Response: `{"labels": ["bug"], "confidence": 0.9, "reasoning": "test"}`,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	completer := NewOllamaCompleter(server.URL, "llama3.1:8b")
	result, err := completer.Complete(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "bug") {
		t.Errorf("expected response to contain 'bug', got %q", result)
	}
}

func TestOllamaEmbedder_URLNormalization(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434/", "nomic-embed-text")
	if embedder.url != "http://localhost:11434" {
		t.Errorf("expected trailing slash to be stripped, got %q", embedder.url)
	}
}

func TestOllamaEmbedder_EmptyText(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
	_, err := embedder.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestOllamaEmbedder_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOllamaCompleter_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	completer := NewOllamaCompleter(server.URL, "llama3.1:8b")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
