package pubsub

import (
	"context"
	"sync"
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
