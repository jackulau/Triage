package retry

import (
	"context"
	"math"
	"math/rand/v2"
	"time"
)

const (
	// DefaultMaxAttempts is the default number of attempts before giving up.
	DefaultMaxAttempts = 3

	// baseDelay is the initial backoff delay.
	baseDelay = 1 * time.Second

	// maxDelay caps the backoff delay.
	maxDelay = 10 * time.Second

	// jitterFraction is the maximum fraction of the delay added as jitter.
	jitterFraction = 0.25
)

// Do retries fn up to maxAttempts times with exponential backoff and jitter.
// It respects context cancellation and returns the last error if all attempts fail.
// The backoff progression is: 1s, 2s, 4s (with up to 25% jitter).
func Do(ctx context.Context, maxAttempts int, fn func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't sleep after the last attempt.
		if attempt < maxAttempts-1 {
			delay := backoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return lastErr
}

// backoff calculates the delay for the given attempt (0-indexed) with jitter.
// Progression: 1s, 2s, 4s, ... capped at maxDelay.
func backoff(attempt int) time.Duration {
	delay := time.Duration(math.Pow(2, float64(attempt))) * baseDelay
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter: up to jitterFraction of the delay.
	jitter := time.Duration(float64(delay) * jitterFraction * rand.Float64())
	return delay + jitter
}
