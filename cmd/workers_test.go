package cmd

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWorkerPoolPattern validates that the semaphore-based worker pool
// pattern used in scan correctly limits concurrency.
func TestWorkerPoolPattern(t *testing.T) {
	const numItems = 20
	const maxWorkers = 3

	var concurrent int64
	var maxConcurrent int64
	var completed int64

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numItems; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			cur := atomic.AddInt64(&concurrent, 1)
			// Track maximum concurrency seen
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if cur <= old {
					break
				}
				if atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
					break
				}
			}

			// Simulate work
			time.Sleep(5 * time.Millisecond)

			atomic.AddInt64(&concurrent, -1)
			atomic.AddInt64(&completed, 1)
		}()
	}
	wg.Wait()

	finalCompleted := atomic.LoadInt64(&completed)
	if finalCompleted != numItems {
		t.Errorf("expected %d completed, got %d", numItems, finalCompleted)
	}

	finalMaxConcurrent := atomic.LoadInt64(&maxConcurrent)
	if finalMaxConcurrent > maxWorkers {
		t.Errorf("max concurrent %d exceeded worker limit %d", finalMaxConcurrent, maxWorkers)
	}
	if finalMaxConcurrent == 0 {
		t.Error("expected some concurrency, got 0")
	}

	finalConcurrent := atomic.LoadInt64(&concurrent)
	if finalConcurrent != 0 {
		t.Errorf("expected 0 concurrent at end, got %d", finalConcurrent)
	}
}

// TestWorkerPoolPattern_SingleWorker validates that workers=1 processes sequentially.
func TestWorkerPoolPattern_SingleWorker(t *testing.T) {
	const numItems = 5
	const maxWorkers = 1

	var concurrent int64
	var maxConcurrent int64

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numItems; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			cur := atomic.AddInt64(&concurrent, 1)
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if cur <= old {
					break
				}
				if atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
					break
				}
			}

			time.Sleep(1 * time.Millisecond)
			atomic.AddInt64(&concurrent, -1)
		}()
	}
	wg.Wait()

	if atomic.LoadInt64(&maxConcurrent) > 1 {
		t.Errorf("expected max concurrency 1, got %d", atomic.LoadInt64(&maxConcurrent))
	}
}
