---
id: pipeline-cli
name: Pipeline Wiring and CLI Commands
wave: 3
priority: 1
dependencies: [github-poller, dedup-engine, classifier, notifier]
estimated_hours: 5
tags: [backend, cli, integration, pipeline]
---

## Objective

Wire all components into the triage pipeline (poller -> dedup -> classify -> notify) and implement all remaining CLI commands (watch, scan, check, apply) with graceful shutdown and structured logging.

## Context

This is the integration task that brings everything together. It subscribes to the pub/sub broker for issue events, runs the triage pipeline (snapshot diff -> dedup -> classify -> notify), and exposes all CLI commands. It depends on all Wave 2 tasks being complete since it imports and orchestrates every component.

## Implementation

### 1. Pipeline (`internal/pipeline/pipeline.go`)
1. `Pipeline` struct with all dependencies: poller, dedup engine, classifier, notifier, store, broker
2. `New(deps PipelineDeps) *Pipeline` — constructor with dependency injection
3. `Run(ctx context.Context) error` — subscribe to broker, process events:
   - Receive `IssueEvent` from broker
   - Only process ChangeNew, ChangeTitleEdited, ChangeBodyEdited
   - Step 1: Run dedup engine on the issue
   - Step 2: If not duplicate, run classifier
   - Step 3: Build TriageResult combining dedup + classification results
   - Step 4: Log result in triage_log via store
   - Step 5: Send notification via notifier
4. Error handling per spec:
   - Embedding failure: log, skip issue, will retry on next poll
   - LLM failure: log, send notification with dedup results only (no classification)
   - Notification failure: log, retry once, mark as failed
5. Structured logging with `log/slog` throughout

### 2. Watch Command (`cmd/watch.go`)
1. `triage watch <owner/repo>` — continuous poll + triage loop
2. Flags: `--interval 5m`, `--notify slack|discord|both`, `--dry-run`, `--verbose`
3. Initialize all components from config
4. Start poller in background goroutine
5. Start pipeline in background goroutine
6. Graceful shutdown on SIGINT/SIGTERM:
   - Cancel context
   - Drain in-flight triage operations
   - Close SQLite connection
   - Log shutdown summary
7. Display real-time status (issue count, last poll time, rate limit remaining)

### 3. Scan Command (`cmd/scan.go`)
1. `triage scan <owner/repo>` — one-shot full scan
2. Fetch ALL open issues (paginate through everything)
3. Compute embeddings for all issues (with progress bar)
4. Run dedup across all issues to find duplicate clusters
5. Classify unlabeled issues (with progress bar)
6. Send results to notification target
7. Display summary at end (issues scanned, duplicates found, classified)

### 4. Check Command (`cmd/check.go`)
1. `triage check <owner/repo#123>` — single issue check
2. Parse `owner/repo#number` format
3. Fetch issue from GitHub API
4. Run dedup against existing index
5. Run classification
6. Print results to stdout (formatted, not notification)

### 5. Apply Command (`cmd/apply.go`)
1. `triage apply <owner/repo#123> [labels...]` — apply labels after human review
2. Parse arguments
3. Use GitHub App client to add labels: `POST /repos/{owner}/{repo}/issues/{number}/labels`
4. Log action in triage_log with `human_decision: approved`
5. Print confirmation

### 6. Structured Logging
1. Configure `log/slog` with JSON handler for structured output
2. Include context: repo, issue number, action, duration
3. Verbose mode (--verbose) adds rate limit info, embedding details, timing

### 7. Component Initialization (`cmd/root.go` additions)
1. Add `initComponents()` helper that creates all components from config:
   - GitHub client from config auth settings
   - Embedding provider from config
   - LLM provider from config
   - Store from config path
   - Notifier from config webhook URLs
   - Poller, dedup engine, classifier, pipeline
2. Wire everything together with dependency injection

## Acceptance Criteria

- [ ] `triage watch myorg/myrepo` starts polling and processing issues
- [ ] Watch mode shows real-time status output
- [ ] Watch mode shuts down gracefully on Ctrl+C (no orphaned goroutines)
- [ ] `triage scan myorg/myrepo` does full scan with progress indication
- [ ] `triage check myorg/myrepo#123` shows dedup + classification results
- [ ] `triage apply myorg/myrepo#123 bug crash` applies labels via GitHub API
- [ ] Pipeline correctly chains: poller -> dedup -> classify -> notify
- [ ] Embedding API failure is handled: issue skipped, notification still sent (without dedup)
- [ ] LLM API failure is handled: notification sent with dedup results only
- [ ] Notification failure is handled: logged, retried once
- [ ] Structured logs include repo, issue, action, duration
- [ ] --dry-run mode triages but doesn't send notifications
- [ ] --verbose mode shows detailed debug output
- [ ] All CLI commands have proper --help text
- [ ] Integration test: mock all providers, verify pipeline flow end-to-end

## Files to Create/Modify

- `internal/pipeline/pipeline.go` — Pipeline orchestrator
- `internal/pipeline/pipeline_test.go` — Pipeline integration tests
- `cmd/watch.go` — Watch command
- `cmd/scan.go` — Scan command
- `cmd/check.go` — Check command
- `cmd/apply.go` — Apply command
- `cmd/root.go` — Add component initialization (modify existing file from foundation)

## Integration Points

- **Provides**: Complete working CLI application
- **Consumes**: ALL packages — github (poller, client), dedup (engine), classify (classifier), notify (notifier), store (all CRUD), config, pubsub, provider implementations
- **Conflicts**: Modifies `cmd/root.go` (adds component init). Foundation creates the base file; this task extends it. All other files are new.
