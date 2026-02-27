---
id: foundation
name: Project Foundation - Module, Config, Store, PubSub, Core Types
wave: 1
priority: 1
dependencies: []
estimated_hours: 6
tags: [backend, core, infrastructure]
---

## Objective

Establish the Go project skeleton with module initialization, CLI framework, configuration loading, SQLite storage layer, pub/sub broker, and all shared type definitions and provider interfaces.

## Context

This is the foundational task that all other tasks depend on. It creates the Go module, directory structure, and all core packages that other tasks will import. Every other task branches from the commit produced by this task. This means it must be complete, compilable, and well-tested before Wave 2 begins.

The project is "Triage" — a Go CLI that watches GitHub repos for new issues, detects duplicates via AI embeddings, classifies them with LLMs, and sends results to Slack/Discord for human review.

## Implementation

### 1. Project Skeleton
1. Run `go mod init github.com/jacklau/triage` (or appropriate module path)
2. Create the full directory structure per the spec:
   ```
   triage/
   ├── cmd/
   │   ├── root.go
   │   └── init.go
   ├── internal/
   │   ├── pubsub/
   │   ├── github/
   │   ├── dedup/
   │   ├── classify/
   │   ├── notify/
   │   ├── pipeline/
   │   ├── store/
   │   ├── config/
   │   └── provider/
   ├── main.go
   ├── go.mod
   └── go.sum
   ```
3. Create `main.go` with `cobra.Execute()`
4. Create `cmd/root.go` with Cobra root command, global flags (--config, --verbose)
5. Create `cmd/init.go` with interactive setup command (`triage init`)

### 2. Configuration (`internal/config/config.go`)
1. Define config structs matching the YAML spec:
   - `Config` (top-level)
   - `GitHubConfig` (auth, app_id, installation_id, private_key_path/private_key)
   - `ProvidersConfig` (embedding, llm sub-configs)
   - `NotifyConfig` (slack, discord webhooks)
   - `DefaultsConfig` (poll_interval, thresholds, etc.)
   - `StoreConfig` (path)
   - `RepoConfig` (labels, custom_prompt, per-repo overrides)
2. Implement YAML loading with `gopkg.in/yaml.v3`
3. Implement `${VAR}` environment variable expansion (fail fast on missing vars)
4. Add validation (required fields, valid intervals, valid thresholds 0-1)
5. Support config file path via `--config` flag or default `~/.triage/config.yaml`

### 3. SQLite Store (`internal/store/`)
1. `db.go` — SQLite setup using `modernc.org/sqlite`, connection pool, WAL mode
2. Implement migration system using `PRAGMA user_version`
3. Create V1 migration with all tables from spec:
   - `repos` (poll state per repo)
   - `issues` (snapshots + embeddings)
   - `triage_log` (decisions + audit)
   - All indexes
4. `repos.go` — Repo CRUD: CreateRepo, GetRepo, UpdatePollState, ListRepos
5. `issues.go` — Issue CRUD: UpsertIssue, GetIssue, GetIssuesByRepo, UpdateEmbedding, GetEmbeddings
6. `triage.go` — Triage log CRUD: LogTriageAction, GetTriageLog, UpdateHumanDecision

### 4. Pub/Sub Broker (`internal/pubsub/broker.go`)
1. Implement generic `Broker[T]` per the spec
2. `Event[T]` struct with `Type` and `Payload`
3. `EventType` constants: Created, Updated, Deleted
4. `Subscribe(ctx)` returns `<-chan Event[T]`, cleans up on context cancel
5. `Publish(eventType, payload)` broadcasts to all subscribers
6. Thread-safe with `sync.RWMutex`
7. Buffered channels (64) with drop on slow subscriber

### 5. Core Types (`internal/github/types.go`)
1. Define `Issue` struct (Number, Title, Body, State, Author, Labels, CreatedAt, UpdatedAt)
2. Define `ChangeType` enum (ChangeNew, ChangeTitleEdited, ChangeBodyEdited, ChangeStateChanged, ChangeLabelsChanged, ChangeOther)
3. Define `IssueEvent` struct (Repo, Issue, ChangeType)
4. Define `TriageResult` struct (Repo, IssueNumber, Duplicates, SuggestedLabels, Reasoning)
5. Define `DuplicateCandidate` struct (Number, Score)
6. Define `LabelSuggestion` struct (Name, Confidence)

### 6. Provider Interfaces (`internal/provider/provider.go`)
1. Define `Embedder` interface: `Embed(ctx, text string) ([]float32, error)`
2. Define `Completer` interface: `Complete(ctx, prompt string) (string, error)`
3. Define `EmbedderConfig` and `CompleterConfig` structs
4. Define common error types: `ErrRateLimit`, `ErrTimeout`, `ErrInvalidResponse`

### 7. Dependencies
1. `go get` all required packages:
   - `github.com/spf13/cobra`
   - `modernc.org/sqlite`
   - `gopkg.in/yaml.v3`

## Acceptance Criteria

- [ ] `go build ./...` succeeds with zero errors
- [ ] `go vet ./...` passes
- [ ] `triage --help` shows root command help
- [ ] `triage init --help` shows init command help
- [ ] Config loading works with a sample YAML file
- [ ] Config env var expansion works (substitutes `${VAR}`)
- [ ] Config validation rejects missing required fields
- [ ] SQLite database creates successfully with all tables
- [ ] Migration system applies V1 schema correctly
- [ ] All store CRUD operations work (repos, issues, triage_log)
- [ ] Pub/sub broker publishes and receives events correctly
- [ ] Provider interfaces compile and are importable
- [ ] Unit tests pass for config, store, pubsub, and types

## Files to Create/Modify

- `go.mod` — Module initialization
- `main.go` — Entry point
- `cmd/root.go` — Cobra root command
- `cmd/init.go` — Interactive setup command
- `internal/config/config.go` — Config loading + validation
- `internal/config/config_test.go` — Config tests
- `internal/store/db.go` — SQLite setup + migrations
- `internal/store/repos.go` — Repo CRUD
- `internal/store/issues.go` — Issue CRUD
- `internal/store/triage.go` — Triage log CRUD
- `internal/store/store_test.go` — Store tests
- `internal/pubsub/broker.go` — Generic pub/sub broker
- `internal/pubsub/broker_test.go` — Broker tests
- `internal/github/types.go` — Core domain types
- `internal/provider/provider.go` — Embedder + Completer interfaces

## Integration Points

- **Provides**: Go module, all core types, config loading, SQLite store, pub/sub broker, provider interfaces
- **Consumes**: Nothing (no dependencies)
- **Conflicts**: All other tasks branch from this — this task defines the module path and package layout
