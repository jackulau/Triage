package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/jacklau/triage/internal/github"
)

// mockNotifier is a test implementation of Notifier.
type mockNotifier struct {
	called bool
	err    error
}

func (m *mockNotifier) Notify(ctx context.Context, result github.TriageResult) error {
	m.called = true
	return m.err
}

func TestMultiNotifier_NotifyAll(t *testing.T) {
	n1 := &mockNotifier{}
	n2 := &mockNotifier{}

	multi := NewMultiNotifier(n1, n2)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	err := multi.Notify(context.Background(), result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !n1.called {
		t.Error("expected first notifier to be called")
	}
	if !n2.called {
		t.Error("expected second notifier to be called")
	}
}

func TestMultiNotifier_ContinuesOnError(t *testing.T) {
	n1 := &mockNotifier{err: errors.New("n1 failed")}
	n2 := &mockNotifier{}

	multi := NewMultiNotifier(n1, n2)
	result := github.TriageResult{
		Repo:        "owner/repo",
		IssueNumber: 1,
	}

	err := multi.Notify(context.Background(), result)
	if err == nil {
		t.Fatal("expected error from failing notifier")
	}
	if !n1.called {
		t.Error("expected first notifier to be called")
	}
	if !n2.called {
		t.Error("expected second notifier to be called despite first failing")
	}
}

func TestMultiNotifier_ReturnsLastError(t *testing.T) {
	n1 := &mockNotifier{err: errors.New("n1 failed")}
	n2 := &mockNotifier{err: errors.New("n2 failed")}

	multi := NewMultiNotifier(n1, n2)
	result := github.TriageResult{}

	err := multi.Notify(context.Background(), result)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "n2 failed" {
		t.Errorf("expected last error 'n2 failed', got %q", err.Error())
	}
}

func TestNewNotifier_Slack(t *testing.T) {
	n, err := NewNotifier("slack", "https://hooks.slack.com/test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := n.(*SlackNotifier); !ok {
		t.Errorf("expected *SlackNotifier, got %T", n)
	}
}

func TestNewNotifier_Discord(t *testing.T) {
	n, err := NewNotifier("discord", "", "https://discord.com/api/webhooks/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := n.(*DiscordNotifier); !ok {
		t.Errorf("expected *DiscordNotifier, got %T", n)
	}
}

func TestNewNotifier_Both(t *testing.T) {
	n, err := NewNotifier("both", "https://hooks.slack.com/test", "https://discord.com/api/webhooks/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	multi, ok := n.(*MultiNotifier)
	if !ok {
		t.Fatalf("expected *MultiNotifier, got %T", n)
	}
	if len(multi.notifiers) != 2 {
		t.Errorf("expected 2 notifiers, got %d", len(multi.notifiers))
	}
}

func TestNewNotifier_SlackMissingURL(t *testing.T) {
	_, err := NewNotifier("slack", "", "")
	if err == nil {
		t.Fatal("expected error for missing slack URL")
	}
}

func TestNewNotifier_DiscordMissingURL(t *testing.T) {
	_, err := NewNotifier("discord", "", "")
	if err == nil {
		t.Fatal("expected error for missing discord URL")
	}
}

func TestNewNotifier_BothMissingSlack(t *testing.T) {
	_, err := NewNotifier("both", "", "https://discord.com/api/webhooks/test")
	if err == nil {
		t.Fatal("expected error for missing slack URL")
	}
}

func TestNewNotifier_BothMissingDiscord(t *testing.T) {
	_, err := NewNotifier("both", "https://hooks.slack.com/test", "")
	if err == nil {
		t.Fatal("expected error for missing discord URL")
	}
}

func TestNewNotifier_UnsupportedType(t *testing.T) {
	_, err := NewNotifier("email", "", "")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
