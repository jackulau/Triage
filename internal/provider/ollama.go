package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaModel = "llama3.1:8b"
	defaultOllamaURL   = "http://localhost:11434"
)

// OllamaEmbedder implements the Embedder interface using Ollama's local API.
type OllamaEmbedder struct {
	url    string
	model  string
	client *http.Client
}

// NewOllamaEmbedder creates a new Ollama embedding provider.
// Supported models: "nomic-embed-text" (768 dims), "mxbai-embed-large" (1024 dims).
func NewOllamaEmbedder(url, model string) *OllamaEmbedder {
	// Normalize URL: strip trailing slash
	url = strings.TrimRight(url, "/")

	return &OllamaEmbedder{
		url:   url,
		model: model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ollamaEmbeddingRequest is the request body for the Ollama embeddings API.
type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbeddingResponse is the response body from the Ollama embeddings API.
type ollamaEmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed returns a vector embedding for the given text using Ollama's local API.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("cannot embed empty text")
	}

	reqBody := ollamaEmbeddingRequest{
		Model:  e.model,
		Prompt: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling ollama request: %w", err)
	}

	endpoint := e.url + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return nil, fmt.Errorf("ollama embedding request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: ollama returned 429", ErrRateLimit)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decoding ollama response: %v", ErrInvalidResponse, err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("%w: no embedding returned from ollama", ErrInvalidResponse)
	}

	// Convert float64 to float32
	embedding := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// EmbedBatch returns vector embeddings for multiple texts by calling Embed sequentially.
// Ollama does not support native batch embedding, so this falls back to sequential processing.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return EmbedBatchSequential(ctx, e, texts)
}

// Verify OllamaEmbedder implements BatchEmbedder.
var _ BatchEmbedder = (*OllamaEmbedder)(nil)

// OllamaCompleter implements the Completer interface using a local Ollama server.
type OllamaCompleter struct {
	url    string
	model  string
	client *http.Client
}

// NewOllamaCompleter creates a new OllamaCompleter.
// If url is empty, it defaults to http://localhost:11434.
// If model is empty, it defaults to llama3.1:8b.
func NewOllamaCompleter(url, model string) *OllamaCompleter {
	if url == "" {
		url = defaultOllamaURL
	}
	if model == "" {
		model = defaultOllamaModel
	}
	return &OllamaCompleter{
		url:   url,
		model: model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type ollamaCompletionRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaCompletionResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// Complete sends a prompt to the Ollama server and returns the text completion.
func (o *OllamaCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := ollamaCompletionRequest{
		Model:  o.model,
		Prompt: prompt,
		Stream: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url+"/api/generate", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: %s", ErrTimeout, ctx.Err())
		}
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == 429 {
		return "", fmt.Errorf("%w: HTTP 429", ErrRateLimit)
	}
	if resp.StatusCode == 408 || resp.StatusCode == 504 {
		return "", fmt.Errorf("%w: HTTP %d", ErrTimeout, resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var ollamaResp ollamaCompletionResponse
	if err := json.Unmarshal(respBytes, &ollamaResp); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidResponse, err)
	}

	if ollamaResp.Error != "" {
		return "", fmt.Errorf("ollama error: %s", ollamaResp.Error)
	}

	return ollamaResp.Response, nil
}
