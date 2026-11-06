package event

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestBrokerEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()

	broker := NewBroker("session-1", entry.NewMemoryStore())

	first, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("first Subscribe() error = %v", err)
	}

	t.Cleanup(first.Close)

	second, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}

	t.Cleanup(second.Close)

	want := struct {
		Value int `json:"value"`
	}{
		Value: 1,
	}
	if err := broker.Emit(
		context.Background(),
		"execution-1",
		"test.event",
		want,
	); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	for _, subscription := range []Subscription{first, second} {
		got := receiveEvent(t, subscription)
		if got.SessionID != "session-1" {
			t.Fatalf("SessionID = %q, want session-1", got.SessionID)
		}

		if got.EntryID != 0 || got.ExecutionID != "execution-1" || got.Type != "test.event" {
			t.Fatalf("event = %#v, want transient test.event", got)
		}

		if string(got.Payload) != `{"value":1}` {
			t.Fatalf("Payload = %s, want encoded value", got.Payload)
		}
	}
}

func TestBrokerCommitAppendsAndEmits(t *testing.T) {
	t.Parallel()

	entries := entry.NewMemoryStore()
	broker := NewBroker("session-1", entries)

	subscription, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	if err := broker.Commit(
		context.Background(),
		"execution-1",
		"test.entry",
		map[string]int{"value": 1},
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	got := receiveEvent(t, subscription)
	if got.EntryID != 1 || got.SessionID != "session-1" || got.ExecutionID != "execution-1" {
		t.Fatalf("event = %#v, want durable entry 1", got)
	}

	stored, err := entries.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(stored) != 1 || stored[0].Type != "test.entry" || string(stored[0].Payload) != `{"value":1}` {
		t.Fatalf("entries = %#v, want published entry", stored)
	}
}

func TestBrokerSubscribeReplaysEntries(t *testing.T) {
	t.Parallel()

	entries := entry.NewMemoryStore()

	appended, err := entries.Append(context.Background(), "session-1", entry.Entry{
		Type:    "test.entry",
		Payload: json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	broker := NewBroker("session-1", entries)

	subscription, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	got := receiveEvent(t, subscription)
	if got.EntryID != appended[0].ID || got.Type != appended[0].Type {
		t.Fatalf("event = %#v, want entry %#v", got, appended[0])
	}
}

func TestBrokerCloseStopsSubscribers(t *testing.T) {
	t.Parallel()

	broker := NewBroker("session-1", entry.NewMemoryStore())

	subscription, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	broker.Close()
	broker.Close()

	select {
	case <-subscription.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription to stop")
	}

	if err := subscription.Err(); !errors.Is(err, ErrClosed) {
		t.Fatalf("subscription Err() = %v, want ErrClosed", err)
	}
}

func TestBrokerRejectsUseAfterClose(t *testing.T) {
	t.Parallel()

	entries := entry.NewMemoryStore()
	broker := NewBroker("session-1", entries)
	broker.Close()

	if _, err := broker.Subscribe(context.Background(), 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe() error = %v, want ErrClosed", err)
	}

	if err := broker.Emit(context.Background(), "execution-1", "test.event", 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("Emit() error = %v, want ErrClosed", err)
	}

	if err := broker.Commit(context.Background(), "execution-1", "test.entry", 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("Commit() error = %v, want ErrClosed", err)
	}

	stored, err := entries.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(stored) != 0 {
		t.Fatalf("entries = %#v, want none", stored)
	}
}
