package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Embedder tests ---

func TestOllamaEmbedder_Success(t *testing.T) {
	want := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		var req ollamaEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model nomic-embed-text, got %s", req.Model)
		}
		if req.Prompt != "hello world" {
			t.Errorf("expected prompt 'hello world', got %q", req.Prompt)
		}

		resp := ollamaEmbeddingResponse{Embedding: want}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	got, err := embedder.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d dimensions, got %d", len(want), len(got))
	}
	for i, v := range want {
		if got[i] != float32(v) {
			t.Errorf("dimension %d: expected %f, got %f", i, v, got[i])
		}
	}
}

func TestOllamaEmbedder_EmptyText(t *testing.T) {
	embedder := NewOllamaEmbedder("http://unused", "test-model")
	_, err := embedder.Embed(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestOllamaEmbedder_HTTPError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if errors.Is(err, ErrRateLimit) {
		t.Error("should not be rate limit error")
	}
}

func TestOllamaEmbedder_HTTPError503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}

func TestOllamaEmbedder_RateLimit429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got: %v", err)
	}
}

func TestOllamaEmbedder_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := embedder.Embed(ctx, "test text")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOllamaEmbedder_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"embedding": not valid json`))
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

func TestOllamaEmbedder_EmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ollamaEmbeddingResponse{Embedding: []float64{}})
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

func TestOllamaEmbedder_NilEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	embedder := NewOllamaEmbedder(srv.URL, "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for nil embedding")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

// --- Completer tests ---

func TestOllamaCompleter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected path /api/generate, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ollamaCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "llama3.1:8b" {
			t.Errorf("expected model llama3.1:8b, got %s", req.Model)
		}
		if req.Stream != false {
			t.Error("expected stream to be false")
		}
		if req.Prompt != "say hello" {
			t.Errorf("expected prompt 'say hello', got %q", req.Prompt)
		}

		resp := ollamaCompletionResponse{Response: "Hello, world!"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "llama3.1:8b")
	got, err := completer.Complete(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", got)
	}
}

func TestOllamaCompleter_DefaultURLAndModel(t *testing.T) {
	c := NewOllamaCompleter("", "")
	if c.url != defaultOllamaURL {
		t.Errorf("expected default URL %s, got %s", defaultOllamaURL, c.url)
	}
	if c.model != defaultOllamaModel {
		t.Errorf("expected default model %s, got %s", defaultOllamaModel, c.model)
	}
}

func TestOllamaCompleter_HTTPError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOllamaCompleter_HTTPError503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
}

func TestOllamaCompleter_RateLimit429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got: %v", err)
	}
}

func TestOllamaCompleter_Timeout408(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 408 response")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got: %v", err)
	}
}

func TestOllamaCompleter_GatewayTimeout504(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for 504 response")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got: %v", err)
	}
}

func TestOllamaCompleter_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := completer.Complete(ctx, "test prompt")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOllamaCompleter_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"response": not valid json`))
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

func TestOllamaCompleter_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := ollamaCompletionResponse{Response: ""}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "test-model")
	got, err := completer.Complete(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	// Empty response is valid (not an error), just empty string
	if got != "" {
		t.Errorf("expected empty response, got %q", got)
	}
}

func TestOllamaCompleter_OllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := ollamaCompletionResponse{Error: "model not found: llama99"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	completer := NewOllamaCompleter(srv.URL, "llama99")
	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error when ollama returns error field")
	}
}

func TestOllamaEmbedder_URLTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}
		resp := ollamaEmbeddingResponse{Embedding: []float64{0.1, 0.2}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// URL with trailing slash should still work
	embedder := NewOllamaEmbedder(srv.URL+"/", "test-model")
	_, err := embedder.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed with trailing-slash URL returned error: %v", err)
	}
}
