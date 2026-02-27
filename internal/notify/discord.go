package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jacklau/triage/internal/github"
)

// DiscordNotifier sends triage notifications to a Discord webhook.
type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordNotifier creates a DiscordNotifier with the given webhook URL.
func NewDiscordNotifier(webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// discordEmbed represents a Discord embed object.
type discordEmbed struct {
	Title  string         `json:"title"`
	URL    string         `json:"url"`
	Color  int            `json:"color"`
	Fields []discordField `json:"fields"`
	Footer *discordFooter `json:"footer,omitempty"`
}

// discordField represents a field in a Discord embed.
type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// discordFooter represents the footer of a Discord embed.
type discordFooter struct {
	Text string `json:"text"`
}

// discordPayload is the top-level Discord webhook payload.
type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// BuildDiscordPayload creates the Discord embed message payload for a triage result.
func BuildDiscordPayload(result github.TriageResult) discordPayload {
	issueURL := fmt.Sprintf("https://github.com/%s/issues/%d",
		result.Repo, result.IssueNumber)

	title := fmt.Sprintf("#%d", result.IssueNumber)

	fields := []discordField{
		{
			Name:   "Labels",
			Value:  FormatLabels(result.SuggestedLabels),
			Inline: true,
		},
		{
			Name:   "Duplicates",
			Value:  FormatDuplicates(result.Duplicates),
			Inline: true,
		},
	}

	if result.Reasoning != "" {
		fields = append(fields, discordField{
			Name:   "Reasoning",
			Value:  result.Reasoning,
			Inline: false,
		})
	}

	embed := discordEmbed{
		Title:  title,
		URL:    issueURL,
		Color:  15158332, // Red for issues
		Fields: fields,
		Footer: &discordFooter{
			Text: fmt.Sprintf("triage - %s", result.Repo),
		},
	}

	return discordPayload{
		Embeds: []discordEmbed{embed},
	}
}

// Notify sends a Discord notification for the given triage result.
// Callers are expected to wrap this with retry logic if needed.
func (d *DiscordNotifier) Notify(ctx context.Context, result github.TriageResult) error {
	payload := BuildDiscordPayload(result)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling discord payload: %w", err)
	}

	return d.post(ctx, body)
}

func (d *DiscordNotifier) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
