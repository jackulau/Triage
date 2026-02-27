# Triage

A Go CLI that watches GitHub repos for new issues, detects duplicates via AI embeddings, classifies non-duplicates with LLM-powered labeling, and sends results to Slack/Discord for human review. No AI slop posted to GitHub — humans approve every action.

---

## Architecture

### Design Principles

1. **Human-in-the-loop** — AI triages, humans approve. Notifications go to Slack/Discord. Nothing is auto-posted to GitHub.
2. **Bring your own model** — Pluggable embedding/LLM providers: OpenAI, Anthropic, Ollama (fully local).
3. **GitHub App auth** — First-class GitHub App support. JWT + installation token flow via `ghinstallation`. 5,000 req/hr per installation, scales across orgs.
4. **Pub/sub internals** — Adapted from opencode's broker pattern. Generic `Broker[T]` decouples event producers (poller) from consumers (dedup, classifier, notifier).
5. **Local-first storage** — SQLite for embeddings, triage history, poll state. No external vector DB. Pure-Go SQLite (`modernc.org/sqlite`), zero CGO.
6. **Efficient polling** — `since` parameter + ETags for conditional requests. 304 responses don't count against rate limits.

### System Flow

```
┌────────────────────────────────────────────────────────────────────────┐
│                            triage CLI                                  │
│                                                                        │
│  ┌────────────┐     ┌──────────────┐     ┌─────────────────────────┐  │
│  │  GitHub     │────>│  Pub/Sub     │────>│  Pipeline               │  │
│  │  Poller     │     │  Broker      │     │                         │  │
│  │            │     │  [Issue]     │     │  1. Snapshot diff        │  │
│  │  - since   │     │  [Triage]    │     │  2. Dedup (embeddings)  │  │
│  │  - ETags   │     │              │     │  3. Classify (LLM)      │  │
│  │  - backoff │     └──────────────┘     │  4. Notify (Slack/DC)   │  │
│  └────────────┘                          └─────────────────────────┘  │
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │  GitHub App Auth (ghinstallation)                              │    │
│  │  JWT signing → installation token → auto-refresh at T-5min    │    │
│  └────────────────────────────────────────────────────────────────┘    │
│                                                                        │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │  Local Store (SQLite, pure Go)                                 │    │
│  │  - issue snapshots + embeddings (BLOB)                         │    │
│  │  - poll state (watermarks, ETags)                              │    │
│  │  - triage log (decisions, human approvals)                     │    │
│  └────────────────────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────────┘
```

### Triage Pipeline (per issue)

```
New/Updated Issue
       │
       ▼
┌─────────────┐    title or body changed?
│ Snapshot     │───── no ──> skip (label/comment/state change only)
│ Diff         │
└──────┬──────┘
       │ yes
       ▼
┌─────────────┐    similarity > threshold?
│ Dedup        │───── yes ──> flag as duplicate ──> notify with candidates
│ Engine       │
└──────┬──────┘
       │ no
       ▼
┌─────────────┐
│ Classifier   │──> suggested labels + confidence + reasoning
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Notifier     │──> Slack/Discord message with:
│              │    - issue link
│              │    - duplicate candidates (if any) with scores
│              │    - suggested labels + confidence
│              │    - "Apply" / "Dismiss" actions
└─────────────┘
```

---

## GitHub App Authentication

### Why GitHub App Over PAT

| | GitHub App | PAT |
|---|---|---|
| Rate limit | 5,000/hr **per installation** (scales with orgs) | 5,000/hr total |
| Permissions | Scoped to exactly what's needed | Broad user permissions |
| Auditability | Actions attributed to the App, not a person | Attributed to user |
| Revocation | Per-installation, no user impact | Revokes user access |
| Token lifetime | 1 hour, auto-renewed | Long-lived (risky) |

### Auth Flow

```
Private Key (PEM)
       │
       ▼
┌─────────────┐  RS256 JWT (iss=AppID, exp=10min)
│ JWT Sign     │────────────────────────────────────┐
└─────────────┘                                     │
                                                    ▼
                                   POST /app/installations/{id}/access_tokens
                                                    │
                                                    ▼
                                        Installation Token (1hr TTL)
                                        Auto-renewed at T-5min by
                                        ghinstallation transport
```

### Implementation

```go
import (
    "github.com/bradleyfalzon/ghinstallation/v2"
    "github.com/google/go-github/v60/github"
)

func NewGitHubClient(appID, installationID int64, privateKey []byte) (*github.Client, error) {
    // ghinstallation implements http.RoundTripper:
    //   - Signs JWTs automatically
    //   - Fetches installation tokens on first request
    //   - Caches in memory, renews at T-5min before expiry
    //   - Thread-safe (sync.Mutex on token refresh)
    transport, err := ghinstallation.New(
        http.DefaultTransport,
        appID,
        installationID,
        privateKey,
    )
    if err != nil {
        return nil, fmt.Errorf("github app transport: %w", err)
    }

    return github.NewClient(&http.Client{Transport: transport}), nil
}
```

### Required Permissions

```yaml
# GitHub App → Settings → Permissions & events
permissions:
  issues: write      # read issues + add/remove labels
  metadata: read     # implicit, always granted

events:
  - issues           # for future webhook support
```

`issues: write` is required because adding labels is a write operation (`POST /repos/{owner}/{repo}/issues/{number}/labels`). `metadata: read` is auto-granted.

### Private Key Storage

| Environment | Approach |
|---|---|
| Local dev | File on disk (`chmod 600`), path in config |
| CI/CD | Environment variable `GITHUB_APP_PRIVATE_KEY` (base64-encoded PEM) |
| Production | Secrets manager (AWS SM, GCP SM, Vault) fetched at startup |

The config supports both:
```yaml
github:
  app_id: 12345
  installation_id: 67890
  private_key_path: ~/.triage/github-app.pem     # file path
  # OR
  private_key: ${GITHUB_APP_PRIVATE_KEY}          # env var (base64 PEM)
```

---

## Efficient GitHub Polling

### Strategy

1. **`since` parameter** — `GET /repos/{owner}/{repo}/issues?since=<watermark>&sort=updated&direction=asc&per_page=100`. Filters server-side to only issues updated after the watermark. Catches both new issues and edits.
2. **ETags / conditional requests** — Send `If-None-Match: <etag>` on first page. If nothing changed, GitHub returns `304 Not Modified` which does NOT count against rate limits. Free check.
3. **Watermark advancement** — After processing a batch, advance watermark to the `updated_at` of the last issue processed. Subtract 2 minutes for clock skew buffer.
4. **Proactive throttling** — When `X-RateLimit-Remaining < 100`, spread remaining requests across time until reset window.

### Detecting What Changed

The `since` parameter returns issues where `updated_at > watermark`, but doesn't say *what* changed. We diff against local snapshots:

```go
type ChangeType int
const (
    ChangeNew          ChangeType = iota  // created_at > last poll
    ChangeTitleEdited                     // title differs from snapshot
    ChangeBodyEdited                      // body hash differs from snapshot
    ChangeStateChanged                    // open/closed transition
    ChangeLabelsChanged                   // label set differs
    ChangeOther                           // comment, assignee, etc.
)
```

**Key insight**: We only re-triage on `ChangeNew`, `ChangeTitleEdited`, and `ChangeBodyEdited`. Label/state/comment changes are recorded but don't trigger the dedup+classify pipeline.

### Rate Limit Handling

```
Response received
       │
       ├── 200 OK ──> process, check X-RateLimit-Remaining
       │                  └── < 100? proactive throttle
       │
       ├── 304 Not Modified ──> skip (free, no rate limit hit)
       │
       ├── 403 Forbidden
       │      ├── Retry-After header? ──> sleep that many seconds
       │      └── X-RateLimit-Remaining == 0? ──> sleep until Reset
       │
       ├── 429 Too Many Requests ──> exponential backoff (1s, 2s, 4s, 8s, max 60s)
       │
       └── 5xx Server Error ──> retry with backoff, max 3 attempts
```

---

## Duplicate Detection

### Approach

1. **Embed** `title + "\n\n" + body` using configurable embedding provider
2. **Store** embedding as BLOB in SQLite alongside issue metadata
3. **Compare** new issue embedding against all stored embeddings using cosine similarity
4. **Threshold** — configurable, default 0.85. Above = potential duplicate.
5. **Return** top N candidates (default 3) with similarity scores

### Cosine Similarity

```go
func CosineSimilarity(a, b []float32) float32 {
    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
```

### Scaling Considerations

For repos with <10,000 issues, brute-force cosine similarity against all embeddings is fast enough (~1ms for 10K comparisons with 1536-dim vectors). No need for approximate nearest neighbors.

For larger repos (>50K issues), consider:
- Partition by time window (only compare against issues from last 12 months)
- Pre-filter by label/category to narrow candidate set
- HNSW index (but adds complexity, defer unless needed)

### Embedding Providers

| Provider | Model | Dimensions | Cost |
|---|---|---|---|
| OpenAI | `text-embedding-3-small` | 1536 | $0.02/1M tokens |
| OpenAI | `text-embedding-3-large` | 3072 | $0.13/1M tokens |
| Ollama | `nomic-embed-text` | 768 | Free (local) |
| Ollama | `mxbai-embed-large` | 1024 | Free (local) |

### Edge Cases

- **Empty body**: Embed title only. Lower confidence in similarity scores.
- **Very long issues**: Truncate to model's context window (8191 tokens for OpenAI small). Keep title + first N chars of body.
- **Closed issues**: Still compare against them. A new issue might duplicate a closed one (user doesn't know it was already fixed). Include closed status in notification.
- **Issue edits**: Re-embed on title/body change. Update stored embedding. Re-run dedup — an edit might resolve or create a duplicate match.
- **Cross-repo**: Future feature. For now, dedup is per-repo only.

---

## Classification

### Approach

Send issue `title + body` to LLM with the repo's configured label set. LLM returns structured JSON with labels, confidence, and reasoning.

### Prompt Design

```
You are a GitHub issue triage assistant for the repository {{.Repo}}.

Classify the following issue into one or more of these labels:
{{range .Labels}}
- {{.Name}}: {{.Description}}
{{end}}

Rules:
- Assign 1-3 labels that best describe the issue
- Set confidence between 0.0 and 1.0
- If the issue is unclear or could be multiple things, set confidence lower
- Provide brief reasoning (1-2 sentences)

Issue #{{.Number}}: {{.Title}}

{{.Body}}

Respond with ONLY this JSON (no markdown fences):
{
  "labels": ["label1", "label2"],
  "confidence": 0.92,
  "reasoning": "Brief explanation of why these labels were chosen"
}
```

### Confidence Gating

| Confidence | Action |
|---|---|
| >= 0.9 | High confidence — show in notification as "suggested" |
| 0.7 - 0.9 | Medium — show as "possible", flag for closer review |
| < 0.7 | Low — show as "uncertain", recommend manual triage |

The CLI never auto-applies labels regardless of confidence. It always goes through notification for human review.

### LLM Providers

| Provider | Model | Notes |
|---|---|---|
| Anthropic | `claude-sonnet-4-20250514` | Good default, fast, structured output |
| OpenAI | `gpt-4o-mini` | Cheap, fast |
| Ollama | `llama3.1:8b` | Free, local, lower quality |

---

## Notifications

### Slack

**Method**: Incoming Webhook (simple) or Slack App with interactive messages (rich).

**Simple webhook message:**
```json
{
  "blocks": [
    {
      "type": "header",
      "text": { "type": "plain_text", "text": "New Issue Needs Triage" }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*<https://github.com/org/repo/issues/42|#42: App crashes on startup>*\nOpened by @user • 2 min ago"
      }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Suggested Labels:* `bug` (0.94), `crash` (0.87)\n*Duplicates:* None found"
      }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "*Reasoning:* Issue describes a crash with stack trace on application startup."
      }
    }
  ]
}
```

**When duplicates are found:**
```
*Potential Duplicates:*
• <#38|Memory leak causes crash> — 91% similar
• <#25|App hangs on boot> — 86% similar
```

### Discord

**Method**: Webhook with rich embeds.

```json
{
  "embeds": [{
    "title": "#42: App crashes on startup",
    "url": "https://github.com/org/repo/issues/42",
    "color": 15158332,
    "fields": [
      { "name": "Labels", "value": "`bug` (94%) `crash` (87%)", "inline": true },
      { "name": "Duplicates", "value": "None", "inline": true },
      { "name": "Reasoning", "value": "Crash with stack trace on startup." }
    ],
    "footer": { "text": "triage • org/repo" }
  }]
}
```

### Notification Deduplication

To avoid spamming the channel:
- Track `(repo, issue_number, action)` in `triage_log`
- Don't re-notify for the same issue unless title/body was edited (re-triaged)
- On re-triage after edit, send a threaded reply (Slack) or reference the original message

---

## Local Store (SQLite)

### Schema

```sql
-- Poll state per repo
CREATE TABLE repos (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    owner           TEXT NOT NULL,
    repo            TEXT NOT NULL,
    last_polled_at  TEXT,           -- ISO 8601 watermark
    etag            TEXT,           -- last ETag for conditional requests
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(owner, repo)
);

-- Issue snapshots + embeddings
CREATE TABLE issues (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id         INTEGER NOT NULL REFERENCES repos(id),
    number          INTEGER NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT,
    body_hash       TEXT,           -- SHA-256 for cheap diff
    state           TEXT NOT NULL,  -- open, closed
    author          TEXT,
    labels          TEXT,           -- JSON array of current labels
    embedding       BLOB,           -- float32 array, binary encoded
    embedding_model TEXT,           -- which model generated the embedding
    created_at      TEXT NOT NULL,  -- issue creation time
    updated_at      TEXT NOT NULL,  -- issue last update time
    embedded_at     TEXT,           -- when we last computed the embedding
    UNIQUE(repo_id, number)
);

-- Index for fast similarity scans
CREATE INDEX idx_issues_repo_state ON issues(repo_id, state);
CREATE INDEX idx_issues_repo_embedded ON issues(repo_id, embedded_at);

-- Triage decisions + audit log
CREATE TABLE triage_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id         INTEGER NOT NULL REFERENCES repos(id),
    issue_number    INTEGER NOT NULL,
    action          TEXT NOT NULL,  -- 'classified', 'duplicate_found', 'labels_applied', 'dismissed'
    duplicate_of    TEXT,           -- JSON array of {number, score} if duplicate
    suggested_labels TEXT,          -- JSON array of {name, confidence}
    reasoning       TEXT,
    notified_via    TEXT,           -- 'slack', 'discord', 'both'
    human_decision  TEXT,           -- 'approved', 'rejected', 'modified', NULL (pending)
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_triage_repo_issue ON triage_log(repo_id, issue_number);
```

### Migrations

Use a simple `user_version` pragma for versioning:

```go
func migrate(db *sql.DB) error {
    var version int
    db.QueryRow("PRAGMA user_version").Scan(&version)

    migrations := []string{
        schemaV1, // initial tables
        schemaV2, // future additions
    }

    for i := version; i < len(migrations); i++ {
        if _, err := db.Exec(migrations[i]); err != nil {
            return fmt.Errorf("migration %d: %w", i+1, err)
        }
        db.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1))
    }
    return nil
}
```

---

## CLI Commands

### `triage watch <owner/repo>`

Subscribe mode. Polls on interval, runs full pipeline on new/changed issues.

```
$ triage watch myorg/myrepo --interval 2m --notify slack
Watching myorg/myrepo (poll every 2m, notify via slack)
[14:30:01] Polling... 3 new issues found
[14:30:02] #142: classified as [bug, crash] (0.94) — notified slack
[14:30:03] #143: potential duplicate of #98 (0.91) — notified slack
[14:30:03] #144: classified as [enhancement] (0.88) — notified slack
[14:32:01] Polling... 304 Not Modified (no changes)
[14:34:01] Polling... 1 updated issue
[14:34:02] #142: title edited, re-triaging... classified as [bug] (0.96)
^C
Shutting down gracefully...
```

Flags:
- `--interval 5m` — poll interval (default from config)
- `--notify slack|discord|both` — notification target
- `--dry-run` — triage but don't send notifications
- `--verbose` — show rate limit info, embedding details

### `triage scan <owner/repo>`

One-shot full scan. Backfills embeddings for all open issues, runs dedup across everything, classifies unlabeled issues.

```
$ triage scan myorg/myrepo
Scanning myorg/myrepo...
Fetching issues... 847 open issues (9 pages)
Computing embeddings... 847/847 [============================] 100%
Running dedup analysis...
Found 12 potential duplicate clusters:
  #142 ↔ #98 (0.91)
  #130 ↔ #87 (0.88)
  ...
Classifying 203 unlabeled issues...
  203/203 [============================] 100%
Results sent to slack.
```

### `triage check <owner/repo#123>`

Check a single issue against the existing index.

```
$ triage check myorg/myrepo#142
Issue #142: "App crashes on startup with NPE"

Duplicate check:
  #98: "NullPointerException on launch" — 91.2% similar
  #67: "Startup crash after update" — 84.7% similar

Classification:
  Labels: bug (0.94), crash (0.91)
  Reasoning: Stack trace shows NPE in initialization path.
```

### `triage apply <owner/repo#123> [labels...]`

Apply labels after human review. Uses the GitHub App to add labels.

```
$ triage apply myorg/myrepo#142 bug crash
Applied labels [bug, crash] to myorg/myrepo#142
```

### `triage init`

Interactive setup. Creates config file, helps configure GitHub App.

```
$ triage init
Welcome to triage setup.
GitHub App ID: 12345
Installation ID: 67890
Private key path: ~/.triage/github-app.pem
Embedding provider (openai/ollama): openai
LLM provider (anthropic/openai/ollama): anthropic
Notification target (slack/discord): slack
Slack webhook URL: https://hooks.slack.com/...
Config written to ~/.triage/config.yaml
```

---

## Configuration

`~/.triage/config.yaml`:

```yaml
github:
  auth: app                              # 'app' or 'pat'
  app_id: 12345
  installation_id: 67890
  private_key_path: ~/.triage/github-app.pem
  # private_key: ${GITHUB_APP_PRIVATE_KEY}  # alternative: base64 PEM from env

providers:
  embedding:
    type: openai                         # openai | ollama
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    # For ollama:
    # type: ollama
    # model: nomic-embed-text
    # url: http://localhost:11434

  llm:
    type: anthropic                      # anthropic | openai | ollama
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
    # For ollama:
    # type: ollama
    # model: llama3.1:8b
    # url: http://localhost:11434

notify:
  slack:
    webhook_url: ${SLACK_WEBHOOK_URL}
    channel: "#triage"                   # for display only (webhook targets a channel)
  discord:
    webhook_url: ${DISCORD_WEBHOOK_URL}

defaults:
  poll_interval: 5m
  similarity_threshold: 0.85
  confidence_threshold: 0.7              # below this, flag as "uncertain"
  max_duplicates_shown: 3
  embed_max_tokens: 8000                 # truncate long issues
  request_timeout: 30s

store:
  path: ~/.triage/triage.db              # SQLite database path

repos:
  myorg/myrepo:
    labels:
      - name: bug
        description: "Something isn't working correctly"
      - name: enhancement
        description: "New feature or improvement request"
      - name: documentation
        description: "Improvements or additions to docs"
      - name: question
        description: "User asking for help or clarification"
      - name: security
        description: "Security vulnerability or concern"
      - name: performance
        description: "Performance degradation or optimization needed"
      - name: good first issue
        description: "Good for newcomers to the project"
    custom_prompt: ""                    # optional extra context for the LLM
    similarity_threshold: 0.82           # override per-repo
```

Environment variable expansion: `${VAR}` references are expanded at config load time. Missing vars cause a startup error (fail fast).

---

## Pub/Sub Broker

Adapted from opencode. Generic, type-safe, context-aware.

```go
package pubsub

type EventType string
const (
    Created  EventType = "created"
    Updated  EventType = "updated"
    Deleted  EventType = "deleted"
)

type Event[T any] struct {
    Type    EventType
    Payload T
}

type Subscriber[T any] interface {
    Subscribe(ctx context.Context) <-chan Event[T]
}

type Publisher[T any] interface {
    Publish(eventType EventType, payload T)
}

type Broker[T any] struct {
    mu   sync.RWMutex
    subs map[chan Event[T]]struct{}
}

func NewBroker[T any]() *Broker[T] {
    return &Broker[T]{subs: make(map[chan Event[T]]struct{})}
}

func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
    ch := make(chan Event[T], 64)
    b.mu.Lock()
    b.subs[ch] = struct{}{}
    b.mu.Unlock()

    go func() {
        <-ctx.Done()
        b.mu.Lock()
        delete(b.subs, ch)
        close(ch)
        b.mu.Unlock()
    }()
    return ch
}

func (b *Broker[T]) Publish(t EventType, payload T) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    event := Event[T]{Type: t, Payload: payload}
    for ch := range b.subs {
        select {
        case ch <- event:
        default: // drop if subscriber is slow
        }
    }
}
```

### Event Types

```go
// Domain events flowing through the system
type IssueEvent struct {
    Repo       string
    Issue      Issue
    ChangeType ChangeType  // new, title_edited, body_edited, etc.
}

type TriageResult struct {
    Repo            string
    IssueNumber     int
    Duplicates      []DuplicateCandidate  // {Number, Score}
    SuggestedLabels []LabelSuggestion     // {Name, Confidence}
    Reasoning       string
}
```

---

## Project Structure

```
triage/
├── cmd/
│   ├── root.go              # Cobra root, global flags, config loading
│   ├── watch.go             # triage watch
│   ├── scan.go              # triage scan
│   ├── check.go             # triage check
│   ├── apply.go             # triage apply
│   └── init.go              # triage init (interactive setup)
├── internal/
│   ├── pubsub/
│   │   └── broker.go        # Generic pub/sub broker
│   ├── github/
│   │   ├── client.go        # GitHub App client factory
│   │   ├── poller.go        # Issue polling + ETags + rate limits
│   │   ├── ratelimit.go     # Rate limit parsing + backoff
│   │   └── types.go         # Issue, Event types
│   ├── dedup/
│   │   ├── engine.go        # Orchestrates embed + compare
│   │   └── similarity.go    # Cosine similarity, threshold logic
│   ├── classify/
│   │   ├── classifier.go    # LLM classification orchestrator
│   │   └── prompt.go        # Prompt templates + structured output parsing
│   ├── notify/
│   │   ├── notifier.go      # Notifier interface
│   │   ├── slack.go         # Slack webhook + block formatting
│   │   └── discord.go       # Discord webhook + embed formatting
│   ├── pipeline/
│   │   └── pipeline.go      # Wires poller → dedup → classify → notify
│   ├── store/
│   │   ├── db.go            # SQLite setup, migrations, connection pool
│   │   ├── issues.go        # Issue CRUD + embedding storage
│   │   ├── repos.go         # Repo + poll state CRUD
│   │   └── triage.go        # Triage log CRUD
│   ├── config/
│   │   └── config.go        # YAML loading + env var expansion + validation
│   └── provider/
│       ├── provider.go      # Embedder + Completer interfaces
│       ├── openai.go        # OpenAI embeddings + chat
│       ├── anthropic.go     # Anthropic chat completions
│       └── ollama.go        # Ollama local embeddings + chat
├── main.go                  # cobra.Execute()
├── go.mod
├── go.sum
└── tasks/
    └── todo.md              # This file
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/google/go-github/v60` | GitHub REST API |
| `github.com/bradleyfalzon/ghinstallation/v2` | GitHub App JWT + installation token |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) |
| `github.com/sashabaranov/go-openai` | OpenAI API (embeddings + chat) |
| `github.com/anthropics/anthropic-sdk-go` | Anthropic API |
| `gopkg.in/yaml.v3` | Config parsing |
| `github.com/sethvargo/go-envconfig` | Environment variable expansion |

No Slack/Discord SDK needed — both use simple HTTP webhook POSTs. `net/http` is sufficient.

---

## Implementation Phases

### Phase 1: Foundation
- [ ] `go mod init`, directory structure, `main.go`
- [ ] Cobra root command + `init` subcommand
- [ ] Config loading (YAML + env var expansion + validation)
- [ ] SQLite store (setup, migrations, connection pool)
- [ ] GitHub App client factory (`ghinstallation` + `go-github`)
- [ ] Pub/sub broker

### Phase 2: Polling + Storage
- [ ] GitHub poller (since, pagination, ETags, rate limit handling)
- [ ] Issue snapshot storage + diff detection
- [ ] `triage scan` command (full backfill)
- [ ] Watermark tracking + incremental polling

### Phase 3: Duplicate Detection
- [ ] Embedding provider interface
- [ ] OpenAI embedding implementation
- [ ] Cosine similarity function
- [ ] Dedup engine (embed → compare → rank candidates)
- [ ] Embedding storage in SQLite (BLOB)
- [ ] `triage check` command

### Phase 4: Classification
- [ ] LLM provider interface
- [ ] Anthropic implementation
- [ ] Prompt template + structured JSON parsing
- [ ] Confidence gating logic

### Phase 5: Notifications
- [ ] Slack webhook (rich block messages)
- [ ] Discord webhook (embed messages)
- [ ] Notification deduplication (don't re-notify unless re-triaged)
- [ ] `triage apply` command

### Phase 6: Watch Mode
- [ ] Pipeline wiring (poller → dedup → classify → notify)
- [ ] `triage watch` with poll loop
- [ ] Graceful shutdown (context cancellation, drain in-flight)
- [ ] Structured logging (slog)

### Phase 7: Polish + Robustness
- [ ] Ollama provider (fully local, no API keys)
- [ ] OpenAI LLM provider (alternative to Anthropic)
- [ ] Exponential backoff with jitter on transient failures
- [ ] Request timeouts on all external calls
- [ ] Input validation + error messages
- [ ] Tests (unit for similarity/prompt, integration for pipeline)
- [ ] README with GitHub App setup instructions

---

## Error Handling Strategy

| Failure | Behavior |
|---|---|
| GitHub API 304 | Skip, no-op (nothing changed) |
| GitHub API 403/429 | Backoff, sleep until rate limit resets, retry |
| GitHub API 5xx | Retry up to 3 times with exponential backoff |
| Embedding API failure | Log error, skip issue, retry on next poll cycle |
| LLM API failure | Log error, send notification without classification (dedup only) |
| Slack/Discord webhook failure | Log error, retry once, mark as failed in triage_log |
| SQLite write failure | Fatal — corrupted state, exit with error |
| Invalid config | Fatal on startup — fail fast with clear message |
| Malformed LLM response | Retry once with stricter prompt, fall back to "uncertain" |

---

## Future Considerations (Not in scope for v1)

- **Webhook mode**: HTTP server that receives GitHub webhook events instead of polling. Eliminates delay, but requires a public endpoint.
- **Slack interactive buttons**: "Apply Labels" / "Mark Duplicate" / "Dismiss" buttons that call back to a small HTTP server and apply actions via the GitHub App. Requires running a server.
- **Cross-repo dedup**: Compare issues across multiple repos in the same org.
- **TUI mode**: Bubble Tea interactive terminal UI for reviewing triage results locally.
- **Learning from decisions**: Feed human approve/reject decisions back to improve prompts or fine-tune classification.
- **Batch embedding**: Batch multiple issues into a single API call for cost efficiency during initial scan.
