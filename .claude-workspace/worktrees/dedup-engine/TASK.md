---
id: dedup-engine
name: Duplicate Detection Engine with Embedding Providers
wave: 2
priority: 2
dependencies: [foundation]
estimated_hours: 4
tags: [backend, ai, embeddings, dedup]
---

## Objective

Implement the duplicate detection engine with embedding provider implementations (OpenAI, Ollama), cosine similarity computation, and candidate ranking.

## Context

This task builds the duplicate detection pipeline. When a new or edited issue arrives, its title and body are embedded into a vector, stored in SQLite, and compared against all existing embeddings via cosine similarity. Issues above the similarity threshold are flagged as potential duplicates with ranked candidates returned.

## Implementation

### 1. Embedding Providers (`internal/provider/`)
1. `openai.go` — OpenAI embedding implementation:
   - Implement `Embedder` interface
   - Use `github.com/sashabaranov/go-openai` SDK
   - Support `text-embedding-3-small` (1536 dims) and `text-embedding-3-large` (3072 dims)
   - Handle rate limits and errors
   - `go get github.com/sashabaranov/go-openai`
2. `ollama.go` — Ollama embedding implementation:
   - Implement `Embedder` interface
   - HTTP client to Ollama API (`POST /api/embeddings`)
   - Support `nomic-embed-text` (768 dims) and `mxbai-embed-large` (1024 dims)
   - No external dependency needed (plain HTTP)

### 2. Cosine Similarity (`internal/dedup/similarity.go`)
1. `CosineSimilarity(a, b []float32) float32` — compute cosine similarity between two vectors
2. Handle edge cases: zero vectors return 0, mismatched dimensions return error
3. Optimize for performance (single-pass computation)

### 3. Dedup Engine (`internal/dedup/engine.go`)
1. `Engine` struct with embedder, store, config (threshold, max candidates)
2. `CheckDuplicate(ctx, repo string, issue Issue) (*DedupResult, error)`:
   - Compose text: `title + "\n\n" + body`
   - Handle edge cases:
     - Empty body: embed title only, flag lower confidence
     - Very long issues: truncate to `embed_max_tokens` (default 8000), keep title + first N chars of body
   - Call embedder to get vector
   - Store/update embedding in SQLite via store
   - Fetch all existing embeddings for the repo from store
   - Compute cosine similarity against each
   - Filter by threshold (default 0.85, configurable per-repo)
   - Return top N candidates (default 3) sorted by score
   - Include closed issues in comparison (user might not know it's already fixed)
3. `DedupResult` struct: `IsDuplicate bool`, `Candidates []DuplicateCandidate`
4. Handle re-embedding on issue edit (title/body change): re-embed and re-run dedup

### 4. Embedding Serialization
1. `EncodeEmbedding([]float32) []byte` — serialize float32 slice to binary BLOB for SQLite
2. `DecodeEmbedding([]byte) []float32` — deserialize BLOB back to float32 slice
3. Use `encoding/binary` with LittleEndian for consistent encoding

## Acceptance Criteria

- [ ] OpenAI embedder produces vectors of correct dimensions
- [ ] Ollama embedder connects to local Ollama instance and produces vectors
- [ ] Cosine similarity returns correct values (test with known vectors)
- [ ] Cosine similarity handles edge cases (zero vectors, different lengths)
- [ ] Dedup engine correctly identifies duplicates above threshold
- [ ] Dedup engine returns top N candidates sorted by score
- [ ] Empty body issues are handled (title-only embedding)
- [ ] Long issues are truncated correctly
- [ ] Closed issues are included in comparison
- [ ] Embedding serialization/deserialization roundtrips correctly
- [ ] Re-embedding on edit updates stored embedding
- [ ] Unit tests pass for similarity, serialization, and engine logic

## Files to Create/Modify

- `internal/provider/openai.go` — OpenAI embedding implementation
- `internal/provider/ollama.go` — Ollama embedding implementation (partial — embedding part)
- `internal/dedup/engine.go` — Dedup orchestrator
- `internal/dedup/similarity.go` — Cosine similarity function
- `internal/dedup/engine_test.go` — Engine tests
- `internal/dedup/similarity_test.go` — Similarity tests
- `internal/provider/openai_test.go` — OpenAI provider tests

## Integration Points

- **Provides**: Dedup engine, embedding provider implementations, cosine similarity
- **Consumes**: Provider interfaces (from foundation), Store (embedding storage), Config (thresholds, provider settings)
- **Conflicts**: Shares `internal/provider/` directory with classifier — this task creates `openai.go` (embedding parts) and `ollama.go` (embedding parts). Classifier adds LLM completion to these files. At merge time, these will need careful combination. To minimize conflicts: this task should add embedding-specific structs/functions clearly separated from completion code.
