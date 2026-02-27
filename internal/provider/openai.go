package provider

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

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
