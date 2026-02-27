---
id: github-poller
name: GitHub App Client and Issue Polling System
wave: 2
priority: 1
dependencies: [foundation]
estimated_hours: 4
tags: [backend, github, polling]
---

## Objective

Implement the GitHub App authentication client and the issue polling system with conditional requests, rate limit handling, and snapshot diffing.

## Context

This task builds the GitHub integration layer. It creates the GitHub App client factory using `ghinstallation` for automatic JWT signing and token refresh, and the poller that efficiently fetches new/updated issues using `since` parameters, ETags for conditional requests, and intelligent rate limit handling. It also implements snapshot diffing to detect what changed (title edit, body edit, label change, etc.).

## Implementation

### 1. GitHub App Client (`internal/github/client.go`)
1. `NewGitHubClient(appID, installationID int64, privateKey []byte) (*github.Client, error)`
2. Use `ghinstallation.New()` for automatic JWT + installation token management
3. Support both file path and base64-encoded PEM private key (from config)
4. Wrap with `go-github` v60 client

### 2. Rate Limit Handler (`internal/github/ratelimit.go`)
1. Parse `X-RateLimit-Remaining`, `X-RateLimit-Reset` headers
2. Proactive throttling when remaining < 100
3. Handle 403 (rate limit exceeded) — sleep until reset
4. Handle 429 (too many requests) — exponential backoff (1s, 2s, 4s, 8s, max 60s)
5. Handle 5xx — retry up to 3 times with exponential backoff
6. Handle 304 Not Modified — skip (free, no rate limit hit)

### 3. Issue Poller (`internal/github/poller.go`)
1. `Poller` struct with GitHub client, store, broker, config
2. `Poll(ctx context.Context) error` — single poll cycle:
   - Read watermark from store (`repos.last_polled_at`)
   - Fetch issues: `GET /repos/{owner}/{repo}/issues?since={watermark}&sort=updated&direction=asc&per_page=100`
   - Send `If-None-Match: {etag}` header for conditional requests
   - Handle pagination (follow `Link` header for next pages)
   - For each issue: diff against local snapshot, determine `ChangeType`
   - Publish `IssueEvent` to broker for new/title-edited/body-edited issues
   - Upsert issue snapshot in store
   - Advance watermark to last issue's `updated_at` minus 2min clock skew buffer
   - Store new ETag
3. `Run(ctx context.Context, interval time.Duration) error` — continuous poll loop
4. Graceful shutdown via context cancellation

### 4. Snapshot Diffing
1. Compare fetched issue against stored snapshot
2. Detect `ChangeType`: new (created_at > last poll), title edited, body edited (SHA-256 hash comparison), state changed, labels changed, other
3. Only trigger triage pipeline for: ChangeNew, ChangeTitleEdited, ChangeBodyEdited

### 5. Dependencies
1. `go get github.com/google/go-github/v60`
2. `go get github.com/bradleyfalzon/ghinstallation/v2`

## Acceptance Criteria

- [ ] GitHub App client creates successfully with valid credentials
- [ ] Poller fetches issues with `since` parameter correctly
- [ ] ETags are sent and 304 responses handled (no rate limit cost)
- [ ] Rate limit headers parsed and proactive throttling works
- [ ] 403/429/5xx responses handled with appropriate backoff
- [ ] Pagination follows all pages
- [ ] Snapshot diffing correctly identifies change types
- [ ] Only ChangeNew/ChangeTitleEdited/ChangeBodyEdited trigger pipeline events
- [ ] Watermark advances correctly with clock skew buffer
- [ ] Continuous poll loop starts/stops cleanly via context
- [ ] Unit tests pass for rate limit parsing, snapshot diffing, change detection

## Files to Create/Modify

- `internal/github/client.go` — GitHub App client factory
- `internal/github/poller.go` — Issue poller with since/ETags/pagination
- `internal/github/ratelimit.go` — Rate limit parsing + backoff logic
- `internal/github/poller_test.go` — Poller tests
- `internal/github/ratelimit_test.go` — Rate limit tests

## Integration Points

- **Provides**: GitHub client, issue polling, IssueEvent publishing to broker
- **Consumes**: Config (GitHub auth), Store (watermarks, issue snapshots), PubSub Broker
- **Conflicts**: Only touches `internal/github/` — avoid `client.go` and `poller.go` in other tasks. `types.go` is created by foundation.
