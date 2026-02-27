package github

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestParseRateLimit(t *testing.T) {
	t.Run("parses valid headers", func(t *testing.T) {
		resetTime := time.Now().Add(10 * time.Minute).Unix()
		resp := &http.Response{
			Header: http.Header{
				"X-Ratelimit-Remaining": []string{"42"},
				"X-Ratelimit-Reset":     []string{fmt.Sprintf("%d", resetTime)},
			},
		}

		info := ParseRateLimit(resp)
		if info == nil {
			t.Fatal("expected non-nil RateLimitInfo")
		}
		if info.Remaining != 42 {
			t.Errorf("expected Remaining=42, got %d", info.Remaining)
		}
		if info.Reset.Unix() != resetTime {
			t.Errorf("expected Reset=%d, got %d", resetTime, info.Reset.Unix())
		}
	})

	t.Run("returns nil for nil response", func(t *testing.T) {
		info := ParseRateLimit(nil)
		if info != nil {
			t.Error("expected nil for nil response")
		}
	})

	t.Run("returns nil for missing headers", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{},
		}
		info := ParseRateLimit(resp)
		if info != nil {
			t.Error("expected nil for missing headers")
		}
	})

	t.Run("handles partial headers", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				"X-Ratelimit-Remaining": []string{"50"},
			},
		}
		info := ParseRateLimit(resp)
		if info == nil {
			t.Fatal("expected non-nil RateLimitInfo")
		}
		if info.Remaining != 50 {
			t.Errorf("expected Remaining=50, got %d", info.Remaining)
		}
	})
}

func TestShouldThrottle(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      bool
	}{
		{"below threshold", 50, true},
		{"at threshold", 100, false},
		{"above threshold", 500, false},
		{"zero remaining", 0, true},
		{"just below threshold", 99, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RateLimitInfo{Remaining: tt.remaining}
			got := info.ShouldThrottle()
			if got != tt.want {
				t.Errorf("ShouldThrottle() with remaining=%d: got %v, want %v",
					tt.remaining, got, tt.want)
			}
		})
	}

	t.Run("nil info returns false", func(t *testing.T) {
		var info *RateLimitInfo
		if info.ShouldThrottle() {
			t.Error("nil RateLimitInfo should not throttle")
		}
	})
}

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 60 * time.Second}, // capped at max
		{10, 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			got := BackoffDuration(tt.attempt)
			if got != tt.want {
				t.Errorf("BackoffDuration(%d) = %s, want %s", tt.attempt, got, tt.want)
			}
		})
	}

	t.Run("negative attempt", func(t *testing.T) {
		got := BackoffDuration(-1)
		if got != 1*time.Second {
			t.Errorf("BackoffDuration(-1) = %s, want 1s", got)
		}
	})
}

func TestHandleRateLimitError(t *testing.T) {
	t.Run("403 with reset header", func(t *testing.T) {
		resetTime := time.Now().Add(30 * time.Second).Unix()
		resp := &http.Response{
			StatusCode: http.StatusForbidden,
			Header: http.Header{
				"X-Ratelimit-Remaining": []string{"0"},
				"X-Ratelimit-Reset":     []string{fmt.Sprintf("%d", resetTime)},
			},
		}

		wait, err := HandleRateLimitError(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should be roughly 30 seconds (allow some variance for test execution time).
		if wait < 25*time.Second || wait > 35*time.Second {
			t.Errorf("expected ~30s wait, got %s", wait)
		}
	})

	t.Run("429 with Retry-After header", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Retry-After": []string{"45"},
			},
		}

		wait, err := HandleRateLimitError(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wait != 45*time.Second {
			t.Errorf("expected 45s wait, got %s", wait)
		}
	})

	t.Run("non-rate-limit status", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
		}

		_, err := HandleRateLimitError(resp)
		if err == nil {
			t.Error("expected error for non-rate-limit response")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := HandleRateLimitError(nil)
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("403 without headers defaults to 60s", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{},
		}

		wait, err := HandleRateLimitError(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wait != 60*time.Second {
			t.Errorf("expected 60s fallback, got %s", wait)
		}
	})
}

func TestIsNotModified(t *testing.T) {
	t.Run("304 response", func(t *testing.T) {
		resp := &http.Response{StatusCode: http.StatusNotModified}
		if !IsNotModified(resp) {
			t.Error("expected true for 304")
		}
	})

	t.Run("200 response", func(t *testing.T) {
		resp := &http.Response{StatusCode: http.StatusOK}
		if IsNotModified(resp) {
			t.Error("expected false for 200")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		if IsNotModified(nil) {
			t.Error("expected false for nil")
		}
	})
}

func TestIsServerError(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{500, true},
		{502, true},
		{503, true},
		{599, true},
		{200, false},
		{404, false},
		{429, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.code}
			if got := IsServerError(resp); got != tt.want {
				t.Errorf("IsServerError(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{403, true},
		{429, true},
		{200, false},
		{500, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.code}
			if got := IsRateLimitError(resp); got != tt.want {
				t.Errorf("IsRateLimitError(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestWaitDuration(t *testing.T) {
	t.Run("future reset time", func(t *testing.T) {
		info := &RateLimitInfo{
			Reset: time.Now().Add(30 * time.Second),
		}
		d := info.WaitDuration()
		if d < 25*time.Second || d > 35*time.Second {
			t.Errorf("expected ~30s, got %s", d)
		}
	})

	t.Run("past reset time returns zero", func(t *testing.T) {
		info := &RateLimitInfo{
			Reset: time.Now().Add(-10 * time.Second),
		}
		d := info.WaitDuration()
		if d != 0 {
			t.Errorf("expected 0, got %s", d)
		}
	})

	t.Run("nil info returns zero", func(t *testing.T) {
		var info *RateLimitInfo
		d := info.WaitDuration()
		if d != 0 {
			t.Errorf("expected 0, got %s", d)
		}
	})
}
