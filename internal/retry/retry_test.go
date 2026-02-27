package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoSucceedsFirstAttempt(t *testing.T) {
	var calls int
	err := Do(context.Background(), 3, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDoSucceedsOnNthAttempt(t *testing.T) {
	var calls int
	targetErr := errors.New("transient error")

	err := Do(context.Background(), 3, func() error {
		calls++
		if calls < 3 {
			return targetErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDoExceedsMaxRetries(t *testing.T) {
	targetErr := errors.New("persistent error")
	var calls int

	err := Do(context.Background(), 3, func() error {
		calls++
		return targetErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, targetErr) {
		t.Errorf("expected target error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDoRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32

	// Cancel after the first attempt.
	go func() {
		// Wait for the first attempt to complete.
		for calls.Load() == 0 {
			time.Sleep(1 * time.Millisecond)
		}
		cancel()
	}()

	err := Do(ctx, 5, func() error {
		calls.Add(1)
		return errors.New("keep trying")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should have been called at most twice (first attempt + possibly one
	// more before context check kicks in).
	if calls.Load() > 2 {
		t.Errorf("expected at most 2 calls, got %d", calls.Load())
	}
}

func TestDoContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	var calls int
	err := Do(ctx, 3, func() error {
		calls++
		return nil
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls != 0 {
		t.Errorf("expected 0 calls with cancelled context, got %d", calls)
	}
}

func TestDoDefaultMaxAttempts(t *testing.T) {
	var calls int
	err := Do(context.Background(), 0, func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != DefaultMaxAttempts {
		t.Errorf("expected %d calls (default), got %d", DefaultMaxAttempts, calls)
	}
}

func TestDoSingleAttempt(t *testing.T) {
	var calls int
	err := Do(context.Background(), 1, func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestBackoffProgression(t *testing.T) {
	// Verify that backoff increases with attempts.
	prev := time.Duration(0)
	for attempt := 0; attempt < 3; attempt++ {
		d := backoff(attempt)
		if d <= prev && attempt > 0 {
			t.Errorf("attempt %d: backoff %v should be > previous %v", attempt, d, prev)
		}
		prev = d
	}
}

func TestBackoffCapped(t *testing.T) {
	d := backoff(100) // Very high attempt number.
	maxWithJitter := maxDelay + time.Duration(float64(maxDelay)*jitterFraction)
	if d > maxWithJitter {
		t.Errorf("backoff %v exceeds max with jitter %v", d, maxWithJitter)
	}
}

func TestBackoffIncludesJitter(t *testing.T) {
	// Run backoff many times for the same attempt; at least some should differ.
	seen := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		d := backoff(1)
		seen[d] = true
	}
	// With jitter, we should see multiple distinct values.
	if len(seen) < 2 {
		t.Error("expected jitter to produce varying backoff durations")
	}
}
