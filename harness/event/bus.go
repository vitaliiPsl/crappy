package event

import (
	"context"
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type Bus struct {
	sessionID string
	entries   entry.Store

	mu          sync.Mutex
	subscribers map[*Subscription]struct{}
}

func NewBus(sessionID string, entries entry.Store) *Bus {
	return &Bus{
		sessionID:   sessionID,
		entries:     entries,
		subscribers: make(map[*Subscription]struct{}),
	}
}

func (b *Bus) Subscribe(ctx context.Context, afterID uint64) (*Subscription, error) {
	subscription := NewSubscription(ctx)

	b.mu.Lock()
	b.subscribers[subscription] = struct{}{}
	b.mu.Unlock()

	go b.removeWhenDone(subscription)

	entries, err := b.entries.List(ctx, b.sessionID, afterID)
	if err != nil {
		subscription.Close()
		b.remove(subscription)

		return nil, err
	}

	subscription.Start(afterID, entries)

	if err := subscription.Err(); err != nil {
		b.remove(subscription)

		return nil, err
	}

	return subscription, nil
}

func (b *Bus) Emit(event Event) {
	event.SessionID = b.sessionID

	b.mu.Lock()
	defer b.mu.Unlock()

	for subscription := range b.subscribers {
		if !subscription.Publish(event) {
			delete(b.subscribers, subscription)
		}
	}
}

func (b *Bus) removeWhenDone(subscription *Subscription) {
	<-subscription.Done()
	b.remove(subscription)
}

func (b *Bus) remove(subscription *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.subscribers, subscription)
}
