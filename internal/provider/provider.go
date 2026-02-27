package provider

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors for provider operations.
var (
	ErrRateLimit       = errors.New("rate limit exceeded")
	ErrTimeout         = errors.New("request timed out")
	ErrInvalidResponse = errors.New("invalid response from provider")
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	// Embed returns a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BatchEmbedder extends Embedder with batch embedding support.
// Providers that support native batch embedding (e.g., OpenAI) should implement this
// for better performance. Other providers can use EmbedBatchSequential as a fallback.
type BatchEmbedder interface {
	Embedder
	// EmbedBatch returns vector embeddings for multiple texts in a single call.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbedBatchSequential implements batch embedding by calling Embed sequentially.
// Use this as a fallback for providers that don't support native batch embedding.
func EmbedBatchSequential(ctx context.Context, embedder Embedder, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		emb, err := embedder.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding text %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}

// Completer generates text completions from a prompt.
type Completer interface {
	// Complete returns a text completion for the given prompt.
	Complete(ctx context.Context, prompt string) (string, error)
}

// EmbedderConfig holds configuration for creating an Embedder.
type EmbedderConfig struct {
	Type   string
	Model  string
	APIKey string
	URL    string
}

// CompleterConfig holds configuration for creating a Completer.
type CompleterConfig struct {
	Type   string
	Model  string
	APIKey string
	URL    string
}
