package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v60/github"

	"github.com/jacklau/triage/internal/pubsub"
	"github.com/jacklau/triage/internal/store"
)

// newTestPoller creates a Poller backed by an httptest server and in-memory store.
// The caller must close the returned httptest.Server.
func newTestPoller(t *testing.T, handler http.Handler) (*Poller, *httptest.Server, *store.DB, *pubsub.Broker[IssueEvent]) {
	t.Helper()

	srv := httptest.NewServer(handler)

	client := gogithub.NewClient(nil)
	baseURL, err := client.BaseURL.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parsing base URL: %v", err)
	}
	client.BaseURL = baseURL

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}

	broker := pubsub.NewBroker[IssueEvent]()

	poller := NewPoller(client, db, broker, "testowner", "testrepo")
	return poller, srv, db, broker
}

// makeGitHubIssueJSON creates a JSON-compatible issue response.
func makeGitHubIssueJSON(number int, title, body, state string, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"number":     number,
		"title":      title,
		"body":       body,
		"state":      state,
		"updated_at": updatedAt.Format(time.RFC3339),
		"created_at": updatedAt.Add(-time.Hour).Format(time.RFC3339),
		"user": map[string]interface{}{
			"login": "testauthor",
		},
		"labels": []map[string]interface{}{
			{"name": "bug"},
		},
	}
}

func TestPollerPagination(t *testing.T) {
	// Page tracking
	var requestCount atomic.Int32

	now := time.Now().UTC().Truncate(time.Second)

	// We need the server URL in the Link header, so we create the server
	// first with a mux that we configure after.
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repos/testowner/testrepo/issues", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		requestCount.Add(1)

		var issues []map[string]interface{}

		switch {
		case page == "" || page == "0" || page == "1":
			// First page: return issues 1-2 with Link to next page
			issues = []map[string]interface{}{
				makeGitHubIssueJSON(1, "Issue 1", "Body 1", "open", now.Add(-2*time.Minute)),
				makeGitHubIssueJSON(2, "Issue 2", "Body 2", "open", now.Add(-time.Minute)),
			}
			// go-github parses Link header to set NextPage on Response
			nextURL := fmt.Sprintf("<%s/repos/testowner/testrepo/issues?page=2>; rel=\"next\"", srv.URL)
			w.Header().Set("Link", nextURL)
		case page == "2":
			// Second page: return issue 3
			issues = []map[string]interface{}{
				makeGitHubIssueJSON(3, "Issue 3", "Body 3", "open", now),
			}
			// No Link header means no more pages
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	// Set up go-github client pointing at the test server.
	client := gogithub.NewClient(nil)
	baseURL, err := client.BaseURL.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parsing base URL: %v", err)
	}
	client.BaseURL = baseURL

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer db.Close()

	broker := pubsub.NewBroker[IssueEvent]()
	poller := NewPoller(client, db, broker, "testowner", "testrepo")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx)

	err = poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}

	// Should have received events for all 3 issues (all new)
	received := 0
	timeout := time.After(2 * time.Second)
	for received < 3 {
		select {
		case <-sub:
			received++
		case <-timeout:
			t.Fatalf("timed out waiting for events, got %d/3", received)
		}
	}

	if received != 3 {
		t.Errorf("expected 3 events for 3 new issues, got %d", received)
	}

	// Verify we made 2 requests (2 pages)
	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 page requests, got %d", got)
	}
}

func TestPollerETag304NotModified(t *testing.T) {
	var requestCount atomic.Int32
	now := time.Now().UTC().Truncate(time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		count := requestCount.Add(1)

		if count == 1 {
			// First request: return an issue with an ETag
			issues := []map[string]interface{}{
				makeGitHubIssueJSON(1, "Issue 1", "Body 1", "open", now),
			}
			w.Header().Set("ETag", `"abc123"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(issues)
			return
		}

		// Second request: check ETag and return 304
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Fallback: return empty
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})

	poller, srv, db, _ := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	// First poll: should succeed and store the ETag
	err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("first Poll() error: %v", err)
	}

	// Second poll: should get 304 Not Modified and return nil error
	err = poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("second Poll() should not error on 304, got: %v", err)
	}

	// Verify two requests were made
	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

func TestPollerWatermarkAdvancement(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	issueTime := now.Add(-10 * time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		issues := []map[string]interface{}{
			makeGitHubIssueJSON(1, "Issue 1", "Body 1", "open", issueTime),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	poller, srv, db, _ := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}

	// Verify watermark was advanced: should be issueTime - watermarkBuffer
	repo, err := db.GetRepoByOwnerRepo("testowner", "testrepo")
	if err != nil {
		t.Fatalf("getting repo: %v", err)
	}

	if repo.LastPolledAt == nil {
		t.Fatal("expected LastPolledAt to be set after poll")
	}

	expectedWatermark := issueTime.Add(-watermarkBuffer)
	// Compare with some tolerance for time parsing
	diff := repo.LastPolledAt.Sub(expectedWatermark)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected watermark near %v, got %v (diff: %v)",
			expectedWatermark, *repo.LastPolledAt, diff)
	}
}

func TestPollerRateLimitBackoff(t *testing.T) {
	var requestCount atomic.Int32

	now := time.Now().UTC().Truncate(time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		count := requestCount.Add(1)

		if count == 1 {
			// First request: return 429 with Retry-After of 0 seconds
			// (so the test completes quickly)
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "API rate limit exceeded",
			})
			return
		}

		// After retry: succeed
		issues := []map[string]interface{}{
			makeGitHubIssueJSON(1, "Issue 1", "Body 1", "open", now),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	poller, srv, db, _ := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := poller.Poll(ctx)
	if err != nil {
		t.Fatalf("Poll() after rate limit retry should succeed, got: %v", err)
	}

	// Should have made at least 2 requests (rate limited + retry)
	if got := requestCount.Load(); got < 2 {
		t.Errorf("expected at least 2 requests (rate limit + retry), got %d", got)
	}
}

func TestPollerAPIErrorResponses(t *testing.T) {
	t.Run("500 Internal Server Error with retries", func(t *testing.T) {
		var requestCount atomic.Int32

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/testowner/testrepo/issues" {
				http.NotFound(w, r)
				return
			}

			requestCount.Add(1)

			// Server error: always fail
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "server error",
			})
		})

		poller, srv, db, _ := newTestPoller(t, handler)
		defer srv.Close()
		defer db.Close()

		// Use a shorter context to avoid waiting through all backoff
		// durations (up to ~7s total). We just need to verify retries happen.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := poller.Poll(ctx)
		if err == nil {
			t.Error("expected error for persistent 500, got nil")
		}

		// Should have retried at least once
		if got := requestCount.Load(); got < 2 {
			t.Errorf("expected multiple retry attempts, got %d", got)
		}
		t.Logf("500 error: made %d requests", requestCount.Load())
	})

	t.Run("403 Forbidden rate limit", func(t *testing.T) {
		var requestCount atomic.Int32

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/testowner/testrepo/issues" {
				http.NotFound(w, r)
				return
			}

			requestCount.Add(1)

			// Simulate rate limit 403
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "API rate limit exceeded",
			})
		})

		poller, srv, db, _ := newTestPoller(t, handler)
		defer srv.Close()
		defer db.Close()

		// Use a short context timeout so we don't wait through all
		// backoff retries (which accumulate to ~7s).
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := poller.Poll(ctx)
		if err == nil {
			t.Error("expected error for persistent 403, got nil")
		}

		// Should have made at least 2 requests (first + at least one retry)
		if got := requestCount.Load(); got < 2 {
			t.Errorf("expected at least 2 requests for 403 retry, got %d", got)
		}
		t.Logf("403 error: made %d requests", requestCount.Load())
	})

	t.Run("429 Too Many Requests", func(t *testing.T) {
		var requestCount atomic.Int32

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/testowner/testrepo/issues" {
				http.NotFound(w, r)
				return
			}

			requestCount.Add(1)

			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "rate limit exceeded",
			})
		})

		poller, srv, db, _ := newTestPoller(t, handler)
		defer srv.Close()
		defer db.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := poller.Poll(ctx)
		if err == nil {
			t.Error("expected error for persistent 429, got nil")
		}

		// Should have retried
		if got := requestCount.Load(); got < 2 {
			t.Errorf("expected at least 2 requests for 429 retry, got %d", got)
		}
		t.Logf("429 error: made %d requests", requestCount.Load())
	})
}

func TestPollerContextCancellation(t *testing.T) {
	// Create a handler that blocks until context is cancelled
	handlerReached := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		// Signal that the handler was reached
		select {
		case handlerReached <- struct{}{}:
		default:
		}

		// Block until request context is done
		<-r.Context().Done()
	})

	poller, srv, db, _ := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- poller.Poll(ctx)
	}()

	// Wait for the handler to be reached
	select {
	case <-handlerReached:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for handler to be reached")
	}

	// Cancel the context
	cancel()

	// Poll should return with a context error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error after context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Poll to return after cancellation")
	}
}

func TestPollerNewIssuePublishesEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		issues := []map[string]interface{}{
			makeGitHubIssueJSON(42, "Test Issue", "Test Body", "open", now),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	poller, srv, db, broker := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx)

	err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}

	// Should have received a ChangeNew event
	select {
	case evt := <-sub:
		if evt.Payload.ChangeType != ChangeNew {
			t.Errorf("expected ChangeNew event, got %s", evt.Payload.ChangeType)
		}
		if evt.Payload.Issue.Number != 42 {
			t.Errorf("expected issue number 42, got %d", evt.Payload.Issue.Number)
		}
		if evt.Payload.Issue.Title != "Test Issue" {
			t.Errorf("expected title 'Test Issue', got %q", evt.Payload.Issue.Title)
		}
		if evt.Payload.Repo != "testowner/testrepo" {
			t.Errorf("expected repo 'testowner/testrepo', got %q", evt.Payload.Repo)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPollerSkipsPullRequests(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		// Return one regular issue and one PR (has pull_request key)
		issues := []map[string]interface{}{
			makeGitHubIssueJSON(1, "Real Issue", "Body", "open", now),
			{
				"number":     2,
				"title":      "A Pull Request",
				"body":       "PR body",
				"state":      "open",
				"updated_at": now.Format(time.RFC3339),
				"created_at": now.Add(-time.Hour).Format(time.RFC3339),
				"user":       map[string]interface{}{"login": "author"},
				"labels":     []map[string]interface{}{},
				"pull_request": map[string]interface{}{
					"url": "https://api.github.com/repos/testowner/testrepo/pulls/2",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	poller, srv, db, broker := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx)

	err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}

	// Should only receive event for the real issue, not the PR
	select {
	case evt := <-sub:
		if evt.Payload.Issue.Number != 1 {
			t.Errorf("expected issue #1, got #%d", evt.Payload.Issue.Number)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for issue event")
	}

	// No more events expected
	select {
	case evt := <-sub:
		t.Errorf("unexpected extra event for issue #%d", evt.Payload.Issue.Number)
	case <-time.After(200 * time.Millisecond):
		// Good: no extra events
	}
}

func TestPollerUpdatedIssueDetected(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var requestCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testowner/testrepo/issues" {
			http.NotFound(w, r)
			return
		}

		count := requestCount.Add(1)

		var issues []map[string]interface{}
		if count == 1 {
			// First poll: original issue
			issues = []map[string]interface{}{
				makeGitHubIssueJSON(1, "Original Title", "Original Body", "open", now),
			}
		} else {
			// Second poll: updated title
			issues = []map[string]interface{}{
				makeGitHubIssueJSON(1, "Updated Title", "Original Body", "open", now.Add(time.Minute)),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(issues)
	})

	poller, srv, db, broker := newTestPoller(t, handler)
	defer srv.Close()
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx)

	// First poll: new issue
	err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("first Poll() error: %v", err)
	}

	select {
	case evt := <-sub:
		if evt.Payload.ChangeType != ChangeNew {
			t.Errorf("expected ChangeNew, got %s", evt.Payload.ChangeType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for new issue event")
	}

	// Second poll: title changed
	err = poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("second Poll() error: %v", err)
	}

	select {
	case evt := <-sub:
		if evt.Payload.ChangeType != ChangeTitleEdited {
			t.Errorf("expected ChangeTitleEdited, got %s", evt.Payload.ChangeType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for title change event")
	}
}
