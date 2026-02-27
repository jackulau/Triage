package notify

import (
	"context"
	"fmt"
	"log"

	"github.com/jacklau/triage/internal/github"
)

// Notifier sends notifications about triage results.
type Notifier interface {
	Notify(ctx context.Context, result github.TriageResult) error
}

// MultiNotifier sends notifications to multiple notifiers.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a MultiNotifier from the given notifiers.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Notify sends the triage result to all configured notifiers.
// It logs errors from individual notifiers but continues to the rest.
// Returns the last error encountered, if any.
func (m *MultiNotifier) Notify(ctx context.Context, result github.TriageResult) error {
	var lastErr error
	for _, n := range m.notifiers {
		if err := n.Notify(ctx, result); err != nil {
			log.Printf("notifier error: %v", err)
			lastErr = err
		}
	}
	return lastErr
}

// NewNotifier creates a Notifier based on the notifyType.
// Supported types: "slack", "discord", "both".
func NewNotifier(notifyType string, slackURL, discordURL string) (Notifier, error) {
	switch notifyType {
	case "slack":
		if slackURL == "" {
			return nil, fmt.Errorf("slack webhook URL is required for slack notifier")
		}
		return NewSlackNotifier(slackURL), nil
	case "discord":
		if discordURL == "" {
			return nil, fmt.Errorf("discord webhook URL is required for discord notifier")
		}
		return NewDiscordNotifier(discordURL), nil
	case "both":
		if slackURL == "" {
			return nil, fmt.Errorf("slack webhook URL is required for 'both' notifier")
		}
		if discordURL == "" {
			return nil, fmt.Errorf("discord webhook URL is required for 'both' notifier")
		}
		return NewMultiNotifier(
			NewSlackNotifier(slackURL),
			NewDiscordNotifier(discordURL),
		), nil
	default:
		return nil, fmt.Errorf("unsupported notifier type: %q", notifyType)
	}
}
