package github

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	// throttleThreshold is the remaining request count below which we throttle.
	throttleThreshold = 100

	// maxBackoff is the maximum backoff duration.
	maxBackoff = 60 * time.Second

	// maxRetries is the maximum number of retries for server errors.
	maxRetries = 3
)

// RateLimitInfo holds parsed rate limit information from GitHub API response headers.
type RateLimitInfo struct {
	Remaining int
	Reset     time.Time
	Observed  time.Time
}

// ParseRateLimit extracts rate limit information from a GitHub API HTTP response.
// Returns nil if the relevant headers are not present.
func ParseRateLimit(resp *http.Response) *RateLimitInfo {
	if resp == nil {
		return nil
	}

	remainingStr := resp.Header.Get("X-RateLimit-Remaining")
	resetStr := resp.Header.Get("X-RateLimit-Reset")

	if remainingStr == "" && resetStr == "" {
		return nil
	}

	info := &RateLimitInfo{
		Observed: time.Now(),
	}

	if remainingStr != "" {
		remaining, err := strconv.Atoi(remainingStr)
		if err == nil {
			info.Remaining = remaining
		}
	}

	if resetStr != "" {
		resetUnix, err := strconv.ParseInt(resetStr, 10, 64)
		if err == nil {
			info.Reset = time.Unix(resetUnix, 0)
		}
	}

	return info
}

// ShouldThrottle returns true when the remaining rate limit is below the
// safety threshold, indicating we should slow down requests.
func (r *RateLimitInfo) ShouldThrottle() bool {
	if r == nil {
		return false
	}
	return r.Remaining < throttleThreshold
}

// WaitDuration returns how long to wait before the rate limit resets.
// Returns zero if the reset time is in the past.
func (r *RateLimitInfo) WaitDuration() time.Duration {
	if r == nil {
		return 0
	}
	d := time.Until(r.Reset)
	if d < 0 {
		return 0
	}
	return d
}

// HandleRateLimitError parses a 403 or 429 response to determine how long
// to wait before retrying. It extracts timing from rate limit headers.
func HandleRateLimitError(resp *http.Response) (time.Duration, error) {
	if resp == nil {
		return 0, fmt.Errorf("nil response")
	}

	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return 0, fmt.Errorf("not a rate limit error: status %d", resp.StatusCode)
	}

	info := ParseRateLimit(resp)
	if info != nil && !info.Reset.IsZero() {
		wait := info.WaitDuration()
		if wait > 0 {
			return wait, nil
		}
	}

	// If we can't determine from headers, check Retry-After header
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		seconds, err := strconv.Atoi(retryAfter)
		if err == nil {
			return time.Duration(seconds) * time.Second, nil
		}
	}

	// Default fallback: wait 60 seconds
	return 60 * time.Second, nil
}

// BackoffDuration calculates exponential backoff duration for the given
// attempt number (0-indexed). The progression is 1s, 2s, 4s, 8s, ... capped
// at maxBackoff (60s).
func BackoffDuration(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// IsNotModified returns true if the response indicates the resource has not
// changed (HTTP 304). This means the request did not count against the rate
// limit.
func IsNotModified(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotModified
}

// IsServerError returns true if the response has a 5xx status code.
func IsServerError(resp *http.Response) bool {
	return resp != nil && resp.StatusCode >= 500 && resp.StatusCode < 600
}

// IsRateLimitError returns true if the response indicates a rate limit error
// (403 or 429).
func IsRateLimitError(resp *http.Response) bool {
	return resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests)
}
