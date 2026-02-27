package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultAnthropicModel = "claude-sonnet-4-20250514"

// AnthropicCompleter implements the Completer interface using the Anthropic API.
type AnthropicCompleter struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicCompleter creates a new AnthropicCompleter.
// If model is empty, it defaults to claude-sonnet-4-20250514.
func NewAnthropicCompleter(apiKey, model string) *AnthropicCompleter {
	if model == "" {
		model = defaultAnthropicModel
	}
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicCompleter{
		client: &client,
		model:  model,
	}
}

// Complete sends a prompt to Anthropic and returns the text completion.
func (a *AnthropicCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		// Check for rate limit errors
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == 429 {
				return "", fmt.Errorf("%w: %s", ErrRateLimit, err)
			}
			if apiErr.StatusCode == 408 || apiErr.StatusCode == 504 {
				return "", fmt.Errorf("%w: %s", ErrTimeout, err)
			}
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: %s", ErrTimeout, ctx.Err())
		}
		return "", fmt.Errorf("anthropic completion: %w", err)
	}

	// Extract text from the response
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("%w: no text content in response", ErrInvalidResponse)
}
