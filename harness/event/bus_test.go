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

	want := struct {
		Value int `json:"value"`
	}{
		Value: 1,
	}
	if err := bus.Emit(
		context.Background(),
		"execution-1",
		"test.event",
		want,
	); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	for _, subscription := range []*Subscription{first, second} {
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

func TestBusCommitAppendsAndEmits(t *testing.T) {
	t.Parallel()

	entries := entry.NewMemoryStore()
	bus := NewBus("session-1", entries)

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	if err := bus.Commit(
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
