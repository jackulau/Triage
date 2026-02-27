package pubsub

import (
	"context"
	"sync"
)

// EventType describes the kind of event.
type EventType string

const (
	Created EventType = "created"
	Updated EventType = "updated"
	Deleted EventType = "deleted"
)

// Event wraps a typed payload with an event type.
type Event[T any] struct {
	Type    EventType
	Payload T
}

// subscriberBufferSize is the channel buffer size for each subscriber.
const subscriberBufferSize = 64

// Broker is a generic, thread-safe publish/subscribe broker.
type Broker[T any] struct {
	mu   sync.RWMutex
	subs map[chan Event[T]]struct{}
}

// NewBroker creates a new Broker.
func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{
		subs: make(map[chan Event[T]]struct{}),
	}
}

// Subscribe creates a new subscription. The returned channel receives events
// until the provided context is cancelled, at which point the channel is
// closed and the subscription is removed.
func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	ch := make(chan Event[T], subscriberBufferSize)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}()

	return ch
}

// Publish broadcasts an event to all active subscribers. If a subscriber's
// buffer is full, the event is dropped for that subscriber (non-blocking).
func (b *Broker[T]) Publish(eventType EventType, payload T) {
	evt := Event[T]{Type: eventType, Payload: payload}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subs {
		select {
		case ch <- evt:
		default:
			// Drop event for slow subscriber
		}
	}
}
