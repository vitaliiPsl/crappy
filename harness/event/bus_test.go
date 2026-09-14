package event

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestBusEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()

	bus := NewBus("session-1", entry.NewMemoryStore())

	first, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("first Subscribe() error = %v", err)
	}

	t.Cleanup(first.Close)

	second, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}

	t.Cleanup(second.Close)

	want := Event{
		Type:    "test.event",
		Payload: json.RawMessage(`{"value":1}`),
	}
	bus.Emit(want)

	for _, subscription := range []*Subscription{first, second} {
		got := receiveEvent(t, subscription)
		if got.SessionID != "session-1" {
			t.Fatalf("SessionID = %q, want session-1", got.SessionID)
		}

		if got.Type != want.Type || string(got.Payload) != string(want.Payload) {
			t.Fatalf("event = %#v, want %#v", got, want)
		}
	}
}

func TestBusSubscribeReplaysEntries(t *testing.T) {
	t.Parallel()

	entries := entry.NewMemoryStore()

	appended, err := entries.Append(context.Background(), entry.Entry{
		SessionID: "session-1",
		Type:      "test.entry",
		Payload:   json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	bus := NewBus("session-1", entries)

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	got := receiveEvent(t, subscription)
	if got.EntryID != appended[0].ID || got.Type != appended[0].Type {
		t.Fatalf("event = %#v, want entry %#v", got, appended[0])
	}
}
