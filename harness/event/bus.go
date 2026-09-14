package event

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type Bus struct {
	sessionID string
	entries   entry.Store

	publishMu   sync.Mutex
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

func (b *Bus) Emit(
	ctx context.Context,
	executionID, eventType string,
	payload any,
) error {
	events, err := b.encode(ctx, executionID, eventType, payload)
	if err != nil {
		return err
	}

	b.emit(events[0])

	return nil
}

func (b *Bus) Commit(
	ctx context.Context,
	executionID, entryType string,
	payloads ...any,
) error {
	events, err := b.encode(ctx, executionID, entryType, payloads...)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	b.publishMu.Lock()
	defer b.publishMu.Unlock()

	entries := make([]entry.Entry, len(events))
	for i, value := range events {
		entries[i] = entry.Entry{
			SessionID:   value.SessionID,
			ExecutionID: value.ExecutionID,
			Type:        value.Type,
			Payload:     value.Payload,
		}
	}

	appended, err := b.entries.Append(ctx, entries...)
	if err != nil {
		return err
	}

	if len(appended) != len(events) {
		return fmt.Errorf("append %s entries: got %d entries for %d payloads", entryType, len(appended), len(events))
	}

	for i := range appended {
		events[i] = FromEntry(appended[i])
	}

	for _, value := range events {
		b.emit(value)
	}

	return nil
}

func (b *Bus) encode(
	ctx context.Context,
	executionID, eventType string,
	payloads ...any,
) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	events := make([]Event, len(payloads))
	for i, payload := range payloads {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s event: %w", eventType, err)
		}

		events[i] = Event{
			SessionID:   b.sessionID,
			ExecutionID: executionID,
			Type:        eventType,
			Payload:     encoded,
		}
	}

	return events, nil
}

func (b *Bus) emit(event Event) {
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
