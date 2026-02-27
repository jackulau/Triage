package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultOllamaModel = "llama3.1:8b"
	defaultOllamaURL   = "http://localhost:11434"
)

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
		url:    url,
		model:  model,
		client: &http.Client{},
	}
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// Complete sends a prompt to the Ollama server and returns the text completion.
func (o *OllamaCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := ollamaRequest{
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
	defer resp.Body.Close()

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

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBytes, &ollamaResp); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidResponse, err)
	}

	if ollamaResp.Error != "" {
		return "", fmt.Errorf("ollama error: %s", ollamaResp.Error)
	}

	return ollamaResp.Response, nil
}
