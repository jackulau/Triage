---
id: notifier
name: Slack and Discord Notification System
wave: 2
priority: 3
dependencies: [foundation]
estimated_hours: 3
tags: [backend, notifications, slack, discord]
---

## Objective

Implement the notification system with Slack webhook (rich Block Kit messages) and Discord webhook (embed messages), including notification deduplication to avoid spam.

## Context

After triage (dedup + classification), results are sent to Slack and/or Discord for human review. No AI actions are auto-posted to GitHub — the notifications inform humans who then decide whether to apply labels, mark duplicates, or dismiss. This task builds the notification formatters and sending logic, using simple HTTP webhook POSTs (no SDK needed).

## Implementation

### 1. Notifier Interface (`internal/notify/notifier.go`)
1. `Notifier` interface: `Notify(ctx, result TriageResult) error`
2. `NotifierConfig` struct with webhook URL, channel info
3. Factory function: `NewNotifier(notifyType string, config NotifierConfig) (Notifier, error)`
4. Support `slack`, `discord`, and `both` notification targets

### 2. Slack Notifier (`internal/notify/slack.go`)
1. Implement `Notifier` interface
2. Format messages using Slack Block Kit JSON (per spec):
   - Header block: "New Issue Needs Triage" (or "Issue Re-Triaged" for edits)
   - Section: issue link, author, time ago
   - Section: suggested labels with confidence scores
   - Section: duplicate candidates with similarity percentages (if any)
   - Section: reasoning from classifier
3. HTTP POST to webhook URL with JSON payload
4. Handle webhook errors (non-2xx responses)
5. Timeout on HTTP request (default 10s)

### 3. Discord Notifier (`internal/notify/discord.go`)
1. Implement `Notifier` interface
2. Format messages using Discord embed JSON (per spec):
   - Title: issue number + title, linked to GitHub
   - Color: red for bugs (15158332), blue for features, etc.
   - Fields: Labels (inline), Duplicates (inline), Reasoning
   - Footer: "triage - org/repo"
3. HTTP POST to webhook URL with JSON payload
4. Handle webhook errors
5. Timeout on HTTP request (default 10s)

### 4. Notification Deduplication
1. Before sending, check `triage_log` for existing notification for `(repo, issue_number, action)`
2. Don't re-notify for the same issue unless title/body was edited (re-triaged)
3. On re-triage after edit, note in message that this is an update ("Re-Triaged" header)
4. Log notification attempt in `triage_log` with `notified_via` field

### 5. Message Formatting Helpers
1. `FormatLabels(labels []LabelSuggestion) string` — format as "`bug` (94%), `crash` (87%)"
2. `FormatDuplicates(candidates []DuplicateCandidate) string` — format as "- #38: title — 91% similar"
3. `FormatConfidence(level string) string` — map confidence level to display text
4. `TimeAgo(t time.Time) string` — "2 min ago", "1 hour ago", etc.

## Acceptance Criteria

- [ ] Slack notifier sends correctly formatted Block Kit messages
- [ ] Discord notifier sends correctly formatted embed messages
- [ ] Webhook HTTP POST includes correct Content-Type and payload
- [ ] Non-2xx webhook responses are handled gracefully (logged, retried once)
- [ ] Notification deduplication prevents re-notifying for same issue
- [ ] Re-triaged issues get "Re-Triaged" header in notification
- [ ] `notified_via` field logged correctly in triage_log
- [ ] Both Slack and Discord can be used simultaneously ("both" mode)
- [ ] Formatting helpers produce correct output
- [ ] HTTP timeouts are respected
- [ ] Unit tests pass for message formatting and dedup logic

## Files to Create/Modify

- `internal/notify/notifier.go` — Notifier interface + factory
- `internal/notify/slack.go` — Slack webhook implementation
- `internal/notify/discord.go` — Discord webhook implementation
- `internal/notify/format.go` — Shared formatting helpers
- `internal/notify/slack_test.go` — Slack message format tests
- `internal/notify/discord_test.go` — Discord message format tests
- `internal/notify/notifier_test.go` — Dedup and integration tests

## Integration Points

- **Provides**: Notification sending to Slack/Discord, notification deduplication
- **Consumes**: Core types (TriageResult, DuplicateCandidate, LabelSuggestion from foundation), Store (triage_log for dedup), Config (webhook URLs)
- **Conflicts**: Only touches `internal/notify/` — no overlap with other tasks
