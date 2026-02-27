package provider

import (
	"context"
	"errors"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

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
