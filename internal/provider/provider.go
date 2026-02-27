package provider

import (
	"context"
	"errors"
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
