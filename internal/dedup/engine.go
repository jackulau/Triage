package dedup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/jacklau/triage/internal/github"
	"github.com/jacklau/triage/internal/provider"
	"github.com/jacklau/triage/internal/store"
)

// EmbeddingStore is the subset of store.Store used by the dedup engine.
type EmbeddingStore interface {
	GetEmbeddingsForRepo(repoID int64) ([]store.IssueEmbedding, error)
	UpdateEmbedding(repoID int64, number int, embedding []byte, model string) error
	UpdateEmbeddingWithHash(repoID int64, number int, embedding []byte, model, bodyHash string) error
	GetIssueEmbeddingHash(repoID int64, number int) (hash string, hasEmbedding bool, err error)
	GetIssue(repoID int64, number int) (*store.Issue, error)
}

const (
	defaultThreshold     = float32(0.85)
	defaultMaxCandidates = 3
	defaultMaxChars      = 8000
)

// Engine performs duplicate detection by comparing issue embeddings.
type Engine struct {
	embedder      provider.Embedder
	store         EmbeddingStore
	threshold     float32
	maxCandidates int
	maxChars      int
}

// DedupResult contains the outcome of a duplicate check.
type DedupResult struct {
	IsDuplicate bool
	Candidates  []github.DuplicateCandidate
}

// Option configures an Engine.
type Option func(*Engine)

// WithThreshold sets the cosine similarity threshold for duplicate detection.
func WithThreshold(t float32) Option {
	return func(e *Engine) { e.threshold = t }
}

// WithMaxCandidates sets the maximum number of duplicate candidates to return.
func WithMaxCandidates(n int) Option {
	return func(e *Engine) { e.maxCandidates = n }
}

// WithMaxChars sets the maximum number of characters to embed.
func WithMaxChars(n int) Option {
	return func(e *Engine) { e.maxChars = n }
}

// NewEngine creates a new dedup Engine.
func NewEngine(embedder provider.Embedder, store EmbeddingStore, opts ...Option) *Engine {
	e := &Engine{
		embedder:      embedder,
		store:         store,
		threshold:     defaultThreshold,
		maxCandidates: defaultMaxCandidates,
		maxChars:      defaultMaxChars,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// composeText creates the text to embed from an issue's title and body.
// It truncates to maxChars, preserving the title and as much body as fits.
func (e *Engine) composeText(issue github.Issue) string {
	title := issue.Title
	body := issue.Body

	if body == "" {
		if len(title) > e.maxChars {
			return title[:e.maxChars]
		}
		return title
	}

	text := title + "\n\n" + body
	if len(text) > e.maxChars {
		// Keep title + separator, truncate body to fit within maxChars
		prefix := title + "\n\n"
		remaining := e.maxChars - len(prefix)
		if remaining <= 0 {
			// Title alone exceeds maxChars
			return title[:e.maxChars]
		}
		return prefix + body[:remaining]
	}
	return text
}

// ContentHash computes a SHA-256 hash of the issue's title and body content.
// This is used to determine if an issue's content has changed since it was last embedded.
func ContentHash(title, body string) string {
	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte("\n\n"))
	h.Write([]byte(body))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ComposeText creates the text to embed from an issue's title and body (exported for scan).
func (e *Engine) ComposeText(issue github.Issue) string {
	return e.composeText(issue)
}

// CheckDuplicate embeds an issue and compares it against all existing embeddings
// in the same repo to find potential duplicates.
// It skips re-embedding if the content hash hasn't changed.
func (e *Engine) CheckDuplicate(ctx context.Context, repoID int64, issue github.Issue) (*DedupResult, error) {
	return e.CheckDuplicateWithThreshold(ctx, repoID, issue, 0)
}

// CheckDuplicateWithThreshold is like CheckDuplicate but allows overriding the
// similarity threshold per call. If thresholdOverride is 0, the engine's
// configured threshold is used.
func (e *Engine) CheckDuplicateWithThreshold(ctx context.Context, repoID int64, issue github.Issue, thresholdOverride float32) (*DedupResult, error) {
	threshold := e.threshold
	if thresholdOverride > 0 {
		threshold = thresholdOverride
	}

	// Compose the text and compute content hash
	text := e.composeText(issue)
	hash := ContentHash(issue.Title, issue.Body)

	var embedding []float32

	// Check if we can skip re-embedding (content unchanged)
	storedHash, hasEmbedding, err := e.store.GetIssueEmbeddingHash(repoID, issue.Number)
	if err == nil && hasEmbedding && storedHash == hash && hash != "" {
		// Content unchanged, load existing embedding from store
		storedIssue, err := e.store.GetIssue(repoID, issue.Number)
		if err == nil && len(storedIssue.Embedding) > 0 {
			embedding = DecodeEmbedding(storedIssue.Embedding)
		}
	}

	// If we don't have a cached embedding, compute one
	if embedding == nil {
		embedding, err = e.embedder.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding issue #%d: %w", issue.Number, err)
		}

		// Store the embedding with content hash
		encoded := EncodeEmbedding(embedding)
		if err := e.store.UpdateEmbeddingWithHash(repoID, issue.Number, encoded, "", hash); err != nil {
			return nil, fmt.Errorf("storing embedding for issue #%d: %w", issue.Number, err)
		}
	}

	// Fetch all existing embeddings for the repo
	existing, err := e.store.GetEmbeddingsForRepo(repoID)
	if err != nil {
		return nil, fmt.Errorf("fetching embeddings for repo %d: %w", repoID, err)
	}

	// Compare against each existing embedding (excluding the current issue)
	var candidates []github.DuplicateCandidate
	for _, ie := range existing {
		if ie.Number == issue.Number {
			continue // skip self
		}

		other := DecodeEmbedding(ie.Embedding)
		if len(other) == 0 {
			continue
		}

		score, err := CosineSimilarity(embedding, other)
		if err != nil {
			continue // skip dimension mismatches silently
		}

		if score >= threshold {
			candidates = append(candidates, github.DuplicateCandidate{
				Number: ie.Number,
				Score:  score,
			})
		}
	}

	// Sort by descending score
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Limit to maxCandidates
	if len(candidates) > e.maxCandidates {
		candidates = candidates[:e.maxCandidates]
	}

	return &DedupResult{
		IsDuplicate: len(candidates) > 0,
		Candidates:  candidates,
	}, nil
}
