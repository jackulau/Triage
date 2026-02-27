package dedup

import (
	"math"
	"testing"
)

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	a := []float32{1, 2, 3, 4, 5}
	score, err := CosineSimilarity(a, a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(score)-1.0) > 1e-6 {
		t.Errorf("expected ~1.0 for identical vectors, got %f", score)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(score)) > 1e-6 {
		t.Errorf("expected ~0.0 for orthogonal vectors, got %f", score)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(score)+1.0) > 1e-6 {
		t.Errorf("expected ~-1.0 for opposite vectors, got %f", score)
	}
}

func TestCosineSimilarity_KnownPair(t *testing.T) {
	// cos(45°) ≈ 0.7071
	a := []float32{1, 0}
	b := []float32{1, 1}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := float32(1.0 / math.Sqrt(2.0))
	if math.Abs(float64(score-expected)) > 1e-6 {
		t.Errorf("expected ~%f, got %f", expected, score)
	}
}

func TestCosineSimilarity_ZeroVectorA(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for zero vector, got %f", score)
	}
}

func TestCosineSimilarity_ZeroVectorB(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{0, 0, 0}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for zero vector, got %f", score)
	}
}

func TestCosineSimilarity_BothZeroVectors(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{0, 0, 0}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for both zero vectors, got %f", score)
	}
}

func TestCosineSimilarity_DimensionMismatch(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	a := []float32{}
	b := []float32{}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for empty vectors, got %f", score)
	}
}

func TestCosineSimilarity_NilVectors(t *testing.T) {
	// nil slices should behave like empty slices (len == 0)
	score, err := CosineSimilarity(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for nil vectors, got %f", score)
	}
}

func TestCosineSimilarity_NilAndNonNilMismatch(t *testing.T) {
	// nil (len 0) vs non-empty should return a dimension mismatch error
	_, err := CosineSimilarity(nil, []float32{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for nil vs non-nil dimension mismatch")
	}
}

func TestCosineSimilarity_MismatchedLengthReturnsError(t *testing.T) {
	// Verify that mismatched-length vectors return an error and 0.0 score
	a := []float32{1, 2, 3, 4}
	b := []float32{1, 2}
	score, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected error for mismatched-length vectors")
	}
	if score != 0 {
		t.Errorf("expected 0.0 score for mismatched-length vectors, got %f", score)
	}
}

func TestCosineSimilarity_SingleElementZero(t *testing.T) {
	// Single-element zero vectors
	a := []float32{0}
	b := []float32{0}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 for single-element zero vectors, got %f", score)
	}
}

func TestCosineSimilarity_NearZeroVectors(t *testing.T) {
	// Very small values (near zero but not zero) should not cause issues
	a := []float32{1e-20, 1e-20, 1e-20}
	b := []float32{1e-20, 1e-20, 1e-20}
	score, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// These are identical vectors with very small magnitudes; result should be ~1.0
	if score < 0.99 {
		t.Errorf("expected ~1.0 for identical near-zero vectors, got %f", score)
	}
}
