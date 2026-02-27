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
