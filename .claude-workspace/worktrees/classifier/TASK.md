---
id: classifier
name: LLM-Powered Issue Classification with Label Suggestions
wave: 2
priority: 2
dependencies: [foundation]
estimated_hours: 3
tags: [backend, ai, llm, classification]
---

## Objective

Implement the LLM-powered issue classification system with provider implementations (Anthropic, OpenAI, Ollama for completion), prompt templates, structured JSON parsing, and confidence gating.

## Context

After duplicate detection, non-duplicate issues are classified using an LLM. The classifier sends the issue title and body along with the repo's configured label set to the LLM, which returns structured JSON with suggested labels, confidence scores, and reasoning. Confidence gating determines how the suggestion is presented in notifications (high confidence = "suggested", medium = "possible", low = "uncertain").

## Implementation

### 1. LLM Providers (`internal/provider/`)
1. `anthropic.go` — Anthropic completion implementation:
   - Implement `Completer` interface
   - Use `github.com/anthropics/anthropic-sdk-go`
   - Support `claude-sonnet-4-20250514` (default)
   - Handle rate limits, timeouts, and errors
   - `go get github.com/anthropics/anthropic-sdk-go`
2. Add completion support to `openai.go`:
   - Implement `Completer` interface for OpenAI
   - Support `gpt-4o-mini` model
   - Use existing `go-openai` SDK (already added by dedup-engine)
3. Add completion support to `ollama.go`:
   - Implement `Completer` interface
   - HTTP client to Ollama API (`POST /api/generate`)
   - Support `llama3.1:8b` model

**Note on file sharing with dedup-engine**: Both tasks modify `openai.go` and `ollama.go`. To minimize merge conflicts:
- Name embedding-related types with `Embedding` prefix (e.g., `OpenAIEmbedder`)
- Name completion-related types with `Completion` prefix (e.g., `OpenAICompleter`)
- Keep each implementation in clearly separated sections of the file
- At merge time, both sets of code combine cleanly since they define different types

### 2. Prompt Template (`internal/classify/prompt.go`)
1. Define Go template for classification prompt (per spec)
2. Template variables: `Repo`, `Labels` (name + description), `Number`, `Title`, `Body`
3. Include rules: assign 1-3 labels, confidence 0-1, reasoning
4. Request JSON-only response (no markdown fences)
5. `BuildPrompt(repo string, labels []LabelConfig, issue Issue) (string, error)`

### 3. Classifier (`internal/classify/classifier.go`)
1. `Classifier` struct with completer, config
2. `Classify(ctx, repo string, labels []LabelConfig, issue Issue) (*ClassifyResult, error)`:
   - Build prompt from template
   - Call LLM completer
   - Parse JSON response into `ClassifyResult`
   - Handle malformed JSON: retry once with stricter prompt, fall back to "uncertain"
   - Apply confidence gating:
     - >= 0.9: high confidence ("suggested")
     - 0.7 - 0.9: medium ("possible")
     - < 0.7: low ("uncertain")
3. `ClassifyResult` struct: `Labels []LabelSuggestion`, `Confidence float64`, `Reasoning string`, `ConfidenceLevel string`
4. JSON response parsing with validation (labels must be from configured set)

### 4. Error Handling
1. LLM API failure: return error, caller handles (log + send notification without classification)
2. Malformed response: retry once, then fall back to uncertain
3. Timeout: configurable request timeout (default 30s)

## Acceptance Criteria

- [ ] Anthropic completer sends correct API requests and parses responses
- [ ] OpenAI completer works for chat completions
- [ ] Ollama completer connects to local instance for text generation
- [ ] Prompt template renders correctly with all variables
- [ ] Classifier parses valid JSON responses correctly
- [ ] Classifier handles malformed JSON (retry + fallback)
- [ ] Confidence gating maps scores to correct levels
- [ ] Labels are validated against configured label set
- [ ] Timeouts are respected on LLM calls
- [ ] Unit tests pass for prompt building, JSON parsing, confidence gating

## Files to Create/Modify

- `internal/provider/anthropic.go` — Anthropic LLM implementation
- `internal/provider/openai.go` — Add OpenAI completion (alongside embedding from dedup-engine)
- `internal/provider/ollama.go` — Add Ollama completion (alongside embedding from dedup-engine)
- `internal/classify/classifier.go` — Classification orchestrator
- `internal/classify/prompt.go` — Prompt template
- `internal/classify/classifier_test.go` — Classifier tests
- `internal/classify/prompt_test.go` — Prompt tests
- `internal/provider/anthropic_test.go` — Anthropic provider tests

## Integration Points

- **Provides**: Issue classifier, LLM provider implementations, confidence gating
- **Consumes**: Provider interfaces (from foundation), Config (LLM provider settings, repo labels)
- **Conflicts**: Shares `internal/provider/openai.go` and `internal/provider/ollama.go` with dedup-engine. Use clearly distinct type names (see Note above). `anthropic.go` is exclusive to this task.
