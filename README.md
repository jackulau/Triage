# Triage

AI-powered GitHub issue triage CLI. Watches repositories for new issues, detects duplicates via vector embeddings, classifies with LLMs, and notifies your team on Slack or Discord.

## How It Works

```
GitHub Issues → Poller → Pub/Sub Broker → Pipeline (Dedup → Classify → Notify)
```

1. **Poll** — Fetches new/updated issues from GitHub using ETags for efficiency
2. **Deduplicate** — Computes embeddings and finds similar existing issues via cosine similarity
3. **Classify** — Sends issue context to an LLM to suggest labels with confidence scores
4. **Notify** — Posts results to Slack and/or Discord for human review
5. **Store** — Persists everything in SQLite (WAL mode) for fast local access

## Installation

```bash
go install github.com/jacklau/triage@latest
```

Or build from source:

```bash
git clone https://github.com/jackulau/Triage.git
cd Triage
go build -o triage .
```

Requires Go 1.25+.

## Quick Start

```bash
# Interactive setup — creates ~/.triage/config.yaml
triage init

# Check a single issue
triage check owner/repo#42

# One-shot scan of all open issues
triage scan owner/repo

# Continuous watching
triage watch owner/repo
```

## Commands

| Command | Description |
|---------|-------------|
| `triage init` | Interactive config setup |
| `triage watch [owner/repo ...]` | Continuously poll and triage issues |
| `triage scan <owner/repo>` | One-shot scan of all open issues |
| `triage check <owner/repo#number>` | Inspect a single issue |
| `triage apply <owner/repo#number> [labels...]` | Apply labels to an issue |

### Common Flags

```
--config <path>   Config file (default ~/.triage/config.yaml)
-v, --verbose     Enable debug logging
```

### `watch`

```
--interval 5m     Poll interval
--notify slack    Notification target: slack, discord, or both
--dry-run         Process issues but skip notifications
```

### `scan`

```
--since 24h       Only process issues updated within this duration
--workers 5       Concurrent processing workers
--output json     Structured JSON output
--notify slack    Notification target
```

### `check`

```
--output json     Structured JSON output
```

## Configuration

Config lives at `~/.triage/config.yaml`. Supports `${ENV_VAR}` expansion for secrets.

```yaml
github:
  auth: app
  app_id: "12345"
  installation_id: "67890"
  private_key_path: /path/to/private-key.pem

providers:
  embedding:
    type: openai          # openai or ollama
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
  llm:
    type: openai          # openai, anthropic, or ollama
    model: gpt-4o-mini
    api_key: ${OPENAI_API_KEY}

notify:
  slack_webhook: ${SLACK_WEBHOOK_URL}
  discord_webhook: ${DISCORD_WEBHOOK_URL}

defaults:
  poll_interval: 5m
  similarity_threshold: 0.85
  confidence_threshold: 0.7
  max_duplicates_shown: 3
  request_timeout: 30s

store:
  path: ~/.triage/triage.db

repos:
  - name: owner/repo
    labels:
      - name: bug
        description: Something isn't working
      - name: feature
        description: New feature or request
    custom_prompt: "Additional context for classification..."
    similarity_threshold: 0.9
```

### Providers

| Provider | Embedding | LLM | API Key Required |
|----------|-----------|-----|------------------|
| OpenAI | Yes | Yes | Yes |
| Anthropic | — | Yes | Yes |
| Ollama | Yes | Yes | No (local) |

### Per-Repo Overrides

Each repo in the `repos` list can override:
- **labels** — Custom label set for classification
- **custom_prompt** — Additional LLM context
- **similarity_threshold** — Dedup sensitivity

## Architecture

```
cmd/           CLI commands (Cobra)
internal/
  classify/    LLM-based issue classification
  config/      YAML config with env var expansion
  dedup/       Vector similarity duplicate detection
  github/      GitHub API polling with ETags
  notify/      Slack + Discord webhook notifiers
  pipeline/    Orchestration (dedup → classify → notify)
  provider/    Embedder + Completer interfaces (OpenAI, Anthropic, Ollama)
  pubsub/      Generic typed pub/sub broker
  retry/       Retry with exponential backoff
  store/       SQLite storage with migrations
```
