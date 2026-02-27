package dedup

import (
	"fmt"
	"math"
)

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 for zero vectors, and an error if dimensions don't match.
// Uses a single-pass computation for performance.
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
	}

	if len(a) == 0 {
		return 0, nil
	}

	var dot, normA, normB float64

	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	// Handle zero vectors
	if normA == 0 || normB == 0 {
		return 0, nil
	}

	return float32(dot / math.Sqrt(normA*normB)), nil
}
