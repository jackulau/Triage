package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jacklau/triage/internal/github"
)

// SlackNotifier sends triage notifications to a Slack webhook.
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewSlackNotifier creates a SlackNotifier with the given webhook URL.
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// slackBlock represents a Slack Block Kit block.
type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

// slackText represents a text object in Slack Block Kit.
type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// slackPayload is the top-level Slack message payload.
type slackPayload struct {
	Blocks []slackBlock `json:"blocks"`
}

// BuildSlackPayload creates the Slack Block Kit message payload for a triage result.
func BuildSlackPayload(result github.TriageResult) slackPayload {
	issueLink := fmt.Sprintf("*<https://github.com/%s/issues/%d|#%d>*",
		result.Repo, result.IssueNumber, result.IssueNumber)

	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{
				Type: "plain_text",
				Text: "New Issue Needs Triage",
			},
		},
		{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf(":link: Issue: %s", issueLink),
			},
		},
		{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Suggested Labels:* %s", FormatLabels(result.SuggestedLabels)),
			},
		},
	}

	if len(result.Duplicates) > 0 {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Potential Duplicates:*\n%s", FormatDuplicates(result.Duplicates)),
			},
		})
	}

	if result.Reasoning != "" {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Reasoning:*\n%s", result.Reasoning),
			},
		})
	}

	return slackPayload{Blocks: blocks}
}

// Notify sends a Slack notification for the given triage result.
// Retries once on non-2xx response.
func (s *SlackNotifier) Notify(ctx context.Context, result github.TriageResult) error {
	payload := BuildSlackPayload(result)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	err = s.post(ctx, body)
	if err != nil {
		log.Printf("slack notify failed, retrying: %v", err)
		// Retry once
		err = s.post(ctx, body)
		if err != nil {
			return fmt.Errorf("slack notify failed after retry: %w", err)
		}
	}
	return nil
}

func (s *SlackNotifier) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
