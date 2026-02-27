package pubsub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	broker := NewBroker[string]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := broker.Subscribe(ctx)

	broker.Publish(Created, "hello")

	select {
	case evt := <-ch:
		if evt.Type != Created {
			t.Errorf("expected event type Created, got %s", evt.Type)
		}
		if evt.Payload != "hello" {
			t.Errorf("expected payload 'hello', got %q", evt.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	broker := NewBroker[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := broker.Subscribe(ctx)
	ch2 := broker.Subscribe(ctx)

	broker.Publish(Updated, 42)

	for _, ch := range []<-chan Event[int]{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Payload != 42 {
				t.Errorf("expected payload 42, got %d", evt.Payload)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestContextCancellation(t *testing.T) {
	broker := NewBroker[string]()
	ctx, cancel := context.WithCancel(context.Background())

	ch := broker.Subscribe(ctx)
	cancel()

	// Wait for cleanup goroutine to run
	time.Sleep(50 * time.Millisecond)

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}

	// Verify subscriber was removed
	broker.mu.RLock()
	count := len(broker.subs)
	broker.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 subscribers after cancel, got %d", count)
	}
}

func TestSlowSubscriberDrop(t *testing.T) {
	broker := NewBroker[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := broker.Subscribe(ctx)

	// Fill the buffer (64 events)
	for i := 0; i < subscriberBufferSize+10; i++ {
		broker.Publish(Created, i)
	}

	// Should be able to read exactly subscriberBufferSize events
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != subscriberBufferSize {
		t.Errorf("expected %d events (buffer size), got %d", subscriberBufferSize, count)
	}
}

func TestConcurrentPublish(t *testing.T) {
	broker := NewBroker[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := broker.Subscribe(ctx)

	var wg sync.WaitGroup
	numPublishers := 10
	eventsPerPublisher := 5

	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				broker.Publish(Created, id*100+j)
			}
		}(i)
	}

	wg.Wait()

	// Drain and count
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done2
		}
	}
done2:
	// Some events may have been dropped if buffer filled up,
	// but we should have received at least some
	if count == 0 {
		t.Error("expected to receive at least some events")
	}
	if count > numPublishers*eventsPerPublisher {
		t.Errorf("received more events than published: %d", count)
	}
}

func TestEventTypes(t *testing.T) {
	broker := NewBroker[string]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := broker.Subscribe(ctx)

	broker.Publish(Created, "a")
	broker.Publish(Updated, "b")
	broker.Publish(Deleted, "c")

	expected := []struct {
		typ     EventType
		payload string
	}{
		{Created, "a"},
		{Updated, "b"},
		{Deleted, "c"},
	}

	for _, exp := range expected {
		select {
		case evt := <-ch:
			if evt.Type != exp.typ {
				t.Errorf("expected type %s, got %s", exp.typ, evt.Type)
			}
			if evt.Payload != exp.payload {
				t.Errorf("expected payload %q, got %q", exp.payload, evt.Payload)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

// --- Concurrent stress tests ---

// TestStressConcurrentPublishersAndSubscribers runs 10 publishers and 10
// subscribers concurrently, verifying no races or panics occur and that
// subscribers collectively receive events.
func TestStressConcurrentPublishersAndSubscribers(t *testing.T) {
	broker := NewBroker[int]()

	const numPublishers = 10
	const numSubscribers = 10
	const eventsPerPublisher = 100

	// Create subscribers, each with its own context.
	type subResult struct {
		id    int
		count int
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan subResult, numSubscribers)
	subscriberReady := make(chan struct{}, numSubscribers)

	// Start subscribers.
	for i := 0; i < numSubscribers; i++ {
		go func(id int) {
			ch := broker.Subscribe(ctx)
			subscriberReady <- struct{}{}

			count := 0
			for range ch {
				count++
			}
			results <- subResult{id: id, count: count}
		}(i)
	}

	// Wait for all subscribers to be ready.
	for i := 0; i < numSubscribers; i++ {
		<-subscriberReady
	}

	// Start publishers.
	var pubWg sync.WaitGroup
	for i := 0; i < numPublishers; i++ {
		pubWg.Add(1)
		go func(id int) {
			defer pubWg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				broker.Publish(Created, id*1000+j)
			}
		}(i)
	}

	// Wait for all publishers to finish.
	pubWg.Wait()

	// Cancel all subscribers and collect results.
	cancel()

	totalReceived := 0
	for i := 0; i < numSubscribers; i++ {
		select {
		case r := <-results:
			if r.count == 0 {
				t.Errorf("subscriber %d received zero events", r.id)
			}
			totalReceived += r.count
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for subscriber results")
		}
	}

	totalPublished := numPublishers * eventsPerPublisher
	t.Logf("total published: %d, total received across %d subscribers: %d",
		totalPublished, numSubscribers, totalReceived)

	// Each subscriber should receive at most totalPublished events.
	// In aggregate, we should have received a significant number.
	if totalReceived == 0 {
		t.Error("no events received across all subscribers")
	}
}

// TestStressSubscribeUnsubscribeDuringPublish tests that subscribing and
// unsubscribing while publishing is actively happening does not cause
// races or panics.
func TestStressSubscribeUnsubscribeDuringPublish(t *testing.T) {
	broker := NewBroker[int]()

	const publishDuration = 500 * time.Millisecond
	const subInterval = 10 * time.Millisecond

	// Track that we completed without panics.
	var publishCount atomic.Int64
	var subCount atomic.Int64

	// Publisher goroutine: continuously publish.
	pubCtx, pubCancel := context.WithTimeout(context.Background(), publishDuration)
	defer pubCancel()

	var wg sync.WaitGroup

	// Start 5 publishers.
	for p := 0; p < 5; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-pubCtx.Done():
					return
				default:
					broker.Publish(Created, i)
					publishCount.Add(1)
					i++
				}
			}
		}()
	}

	// Start 5 subscriber churners: subscribe, read a few events, cancel.
	for s := 0; s < 5; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-pubCtx.Done():
					return
				default:
				}

				subCtx, subCancel := context.WithCancel(context.Background())
				ch := broker.Subscribe(subCtx)
				subCount.Add(1)

				// Read a few events then cancel.
				readCount := 0
				timedOut := false
				for readCount < 3 && !timedOut {
					select {
					case _, ok := <-ch:
						if !ok {
							// Channel was closed externally.
							timedOut = true
						} else {
							readCount++
						}
					case <-pubCtx.Done():
						subCancel()
						return
					case <-time.After(subInterval):
						timedOut = true
					}
				}

				subCancel()
				// Give cleanup goroutine time to run.
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	t.Logf("published %d events, created %d subscriptions",
		publishCount.Load(), subCount.Load())

	// Verify broker is in a clean state: all subscriber contexts were
	// cancelled, so subs map should be empty (or close to it).
	time.Sleep(100 * time.Millisecond) // Let cleanup goroutines finish.
	broker.mu.RLock()
	remaining := len(broker.subs)
	broker.mu.RUnlock()

	// Should be zero since all subscriber contexts were cancelled.
	if remaining != 0 {
		t.Errorf("expected 0 remaining subscribers, got %d", remaining)
	}
}

// TestStressCancelDuringActivePublish tests that cancelling a subscriber's
// context while a publish is actively iterating the subscriber map does
// not cause races or deadlocks.
func TestStressCancelDuringActivePublish(t *testing.T) {
	broker := NewBroker[int]()

	const iterations = 1000

	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		broker.Subscribe(ctx)

		// Race: publish and cancel at the same time.
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			broker.Publish(Created, i)
		}()

		go func() {
			defer wg.Done()
			cancel()
		}()

		wg.Wait()
	}

	// Let cleanup goroutines finish.
	time.Sleep(200 * time.Millisecond)

	broker.mu.RLock()
	remaining := len(broker.subs)
	broker.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 remaining subscribers after all cancels, got %d", remaining)
	}
}

// TestStressChannelBackpressure verifies that a slow subscriber does not
// block publishers or other subscribers from making progress.
func TestStressChannelBackpressure(t *testing.T) {
	broker := NewBroker[int]()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fast subscriber: drains immediately.
	fastCh := broker.Subscribe(ctx)
	var fastCount atomic.Int64

	go func() {
		for range fastCh {
			fastCount.Add(1)
		}
	}()

	// Slow subscriber: never reads, simulating backpressure.
	slowCtx, slowCancel := context.WithCancel(context.Background())
	defer slowCancel()
	_ = broker.Subscribe(slowCtx)

	// Publish many events.
	const totalEvents = 500
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < totalEvents/10; j++ {
				broker.Publish(Created, start+j)
			}
		}(i * (totalEvents / 10))
	}

	wg.Wait()

	// Give fast subscriber time to drain.
	time.Sleep(100 * time.Millisecond)

	fast := fastCount.Load()
	t.Logf("fast subscriber received %d/%d events", fast, totalEvents)

	// Fast subscriber should have received events (at least more than the slow one's buffer).
	if fast == 0 {
		t.Error("fast subscriber received zero events, suggesting backpressure from slow subscriber blocked publishing")
	}

	// Publish should not have blocked. If it did, the test would have timed out.
	// The slow subscriber should have its buffer full (subscriberBufferSize events)
	// and all subsequent events dropped for it.

	// Verify the fast subscriber got significantly more than the buffer size,
	// proving that the slow subscriber did not block the broker.
	if fast <= int64(subscriberBufferSize) {
		t.Errorf("fast subscriber only received %d events (buffer size=%d), slow subscriber may be blocking",
			fast, subscriberBufferSize)
	}
}

// TestStressManySubscribersCreatedAndDestroyed creates and destroys many
// subscribers rapidly to stress the subscriber map management.
func TestStressManySubscribersCreatedAndDestroyed(t *testing.T) {
	broker := NewBroker[string]()

	const numGoroutines = 20
	const subsPerGoroutine = 50

	var wg sync.WaitGroup
	var totalSubs atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < subsPerGoroutine; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				ch := broker.Subscribe(ctx)
				totalSubs.Add(1)

				// Publish an event to this subscriber.
				broker.Publish(Created, fmt.Sprintf("g%d-i%d", gid, i))

				// Read the event.
				select {
				case <-ch:
				case <-time.After(100 * time.Millisecond):
				}

				// Cancel the subscription.
				cancel()
			}
		}(g)
	}

	wg.Wait()

	t.Logf("created and destroyed %d subscriptions", totalSubs.Load())

	// Wait for cleanup goroutines.
	time.Sleep(200 * time.Millisecond)

	broker.mu.RLock()
	remaining := len(broker.subs)
	broker.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 remaining subscribers, got %d", remaining)
	}
}
