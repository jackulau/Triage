package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

// newTestClient creates an openai.Client that points at the given test server.
func newTestClient(serverURL string) *openai.Client {
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = serverURL
	return openai.NewClientWithConfig(cfg)
}

// TestOpenAIEmbed_EmptyDataResponse verifies that Embed returns an error (not a panic)
// when the API returns a response with an empty Data array.
func TestOpenAIEmbed_EmptyDataResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.EmbeddingResponse{
			Data: []openai.Embedding{}, // empty data
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	embedder := newOpenAIEmbedderWithClient(client, "text-embedding-3-small")

	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for empty Data response, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

// TestOpenAIEmbed_ValidResponse verifies normal operation with a valid response.
func TestOpenAIEmbed_ValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.EmbeddingResponse{
			Data: []openai.Embedding{
				{Embedding: []float32{0.1, 0.2, 0.3}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	embedder := newOpenAIEmbedderWithClient(client, "text-embedding-3-small")

	vec, err := embedder.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3-element vector, got %d elements", len(vec))
	}
}

// TestOpenAIEmbed_EmptyText verifies that Embed rejects empty text input.
func TestOpenAIEmbed_EmptyText(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-key", "text-embedding-3-small")

	_, err := embedder.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty text, got nil")
	}

	_, err = embedder.Embed(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only text, got nil")
	}
}

// TestOpenAIComplete_EmptyChoicesResponse verifies that Complete returns an error
// (not a panic) when the API returns a response with an empty Choices array.
func TestOpenAIComplete_EmptyChoicesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{}, // empty choices
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	completer := newOpenAICompleterWithClient(client, "gpt-4o-mini")

	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for empty Choices response, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

// TestOpenAIComplete_ValidResponse verifies normal operation with a valid response.
func TestOpenAIComplete_ValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Content: "Hello, world!",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	completer := newOpenAICompleterWithClient(client, "gpt-4o-mini")

	result, err := completer.Complete(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", result)
	}
}

// TestOpenAIComplete_ServerError verifies error handling for server errors.
func TestOpenAIComplete_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	completer := newOpenAICompleterWithClient(client, "gpt-4o-mini")

	_, err := completer.Complete(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for server error response, got nil")
	}
}

// TestOpenAIEmbed_ServerError verifies error handling for server errors during embedding.
func TestOpenAIEmbed_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	embedder := newOpenAIEmbedderWithClient(client, "text-embedding-3-small")

	_, err := embedder.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for server error response, got nil")
	}
}

// TestOpenAIComplete_NullContentResponse verifies that Complete handles a response
// where the content is empty but the choices array is non-empty.
func TestOpenAIComplete_NullContentResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Content: "", // empty content but choices exist
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	completer := newOpenAICompleterWithClient(client, "gpt-4o-mini")

	result, err := completer.Complete(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty content is valid (not a panic), just an empty string
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
