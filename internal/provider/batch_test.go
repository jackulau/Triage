package provider

import (
	"context"
	"fmt"
	"testing"
)

// testEmbedder is a mock Embedder for testing sequential batch fallback.
type testEmbedder struct {
	callCount int
	err       error
}

func (e *testEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.callCount++
	if e.err != nil {
		return nil, e.err
	}
	// Return a deterministic embedding based on text length
	return []float32{float32(len(text)), 0.1, 0.2}, nil
}

func TestEmbedBatchSequential_Success(t *testing.T) {
	embedder := &testEmbedder{}
	texts := []string{"hello", "world", "test"}

	results, err := EmbedBatchSequential(context.Background(), embedder, texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if embedder.callCount != 3 {
		t.Errorf("expected 3 Embed calls, got %d", embedder.callCount)
	}

	// Verify deterministic results
	if results[0][0] != 5.0 { // len("hello") = 5
		t.Errorf("expected first dim to be 5.0, got %f", results[0][0])
	}
	if results[1][0] != 5.0 { // len("world") = 5
		t.Errorf("expected first dim to be 5.0, got %f", results[1][0])
	}
	if results[2][0] != 4.0 { // len("test") = 4
		t.Errorf("expected first dim to be 4.0, got %f", results[2][0])
	}
}

func TestEmbedBatchSequential_Empty(t *testing.T) {
	embedder := &testEmbedder{}

	results, err := EmbedBatchSequential(context.Background(), embedder, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}

	if embedder.callCount != 0 {
		t.Errorf("expected 0 Embed calls, got %d", embedder.callCount)
	}
}

func TestEmbedBatchSequential_Error(t *testing.T) {
	embedder := &testEmbedder{err: fmt.Errorf("embed failed")}
	texts := []string{"hello", "world"}

	_, err := EmbedBatchSequential(context.Background(), embedder, texts)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbedBatchSequential_ContextCancelled(t *testing.T) {
	embedder := &testEmbedder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := EmbedBatchSequential(ctx, embedder, []string{"hello"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
