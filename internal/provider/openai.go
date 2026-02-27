package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// --- Embedder ---

// OpenAIEmbedder implements the Embedder interface using OpenAI's embedding API.
type OpenAIEmbedder struct {
	client *openai.Client
	model  openai.EmbeddingModel
}

// NewOpenAIEmbedder creates a new OpenAI embedding provider.
// Supported models: "text-embedding-3-small" (1536 dims), "text-embedding-3-large" (3072 dims).
func NewOpenAIEmbedder(apiKey, model string) *OpenAIEmbedder {
	client := openai.NewClient(apiKey)

	var embModel openai.EmbeddingModel
	switch model {
	case "text-embedding-3-large":
		embModel = openai.LargeEmbedding3
	default:
		embModel = openai.SmallEmbedding3
	}

	return &OpenAIEmbedder{
		client: client,
		model:  embModel,
	}
}

// Embed returns a vector embedding for the given text using OpenAI's API.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("cannot embed empty text")
	}

	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: e.model,
	})
	if err != nil {
		// Check for rate limit errors by inspecting the error message.
		if strings.Contains(err.Error(), "429") || strings.Contains(strings.ToLower(err.Error()), "rate limit") {
			return nil, fmt.Errorf("%w: %v", ErrRateLimit, err)
		}
		if strings.Contains(err.Error(), "timeout") {
			return nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return nil, fmt.Errorf("openai embedding request: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("%w: no embeddings returned", ErrInvalidResponse)
	}

	return resp.Data[0].Embedding, nil
}

// --- Completer ---

const defaultOpenAIModel = "gpt-4o-mini"

// OpenAICompleter implements the Completer interface using the OpenAI API.
type OpenAICompleter struct {
	client *openai.Client
	model  string
}

// NewOpenAICompleter creates a new OpenAICompleter.
// If model is empty, it defaults to gpt-4o-mini.
func NewOpenAICompleter(apiKey, model string) *OpenAICompleter {
	if model == "" {
		model = defaultOpenAIModel
	}
	client := openai.NewClient(apiKey)
	return &OpenAICompleter{
		client: client,
		model:  model,
	}
}

// Complete sends a prompt to OpenAI and returns the text completion.
func (o *OpenAICompleter) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: o.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		MaxTokens: 1024,
	})
	if err != nil {
		// Check for rate limit errors
		var apiErr *openai.APIError
		if errors.As(err, &apiErr) {
			if apiErr.HTTPStatusCode == 429 {
				return "", fmt.Errorf("%w: %s", ErrRateLimit, err)
			}
			if apiErr.HTTPStatusCode == 408 || apiErr.HTTPStatusCode == 504 {
				return "", fmt.Errorf("%w: %s", ErrTimeout, err)
			}
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: %s", ErrTimeout, ctx.Err())
		}
		return "", fmt.Errorf("openai completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("%w: no choices in response", ErrInvalidResponse)
	}

	return resp.Choices[0].Message.Content, nil
}
