package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestSessionID(t *testing.T) {
	t.Parallel()

	session := New(State{ID: "session-1"}, entry.NewMemoryStore(), nil)

	if got, want := session.ID(), "session-1"; got != want {
		t.Fatalf("ID() = %q, want %q", got, want)
	}
}

func TestSessionEntries(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(context.Background(),
		entry.Entry{
			SessionID: "session-1",
			Type:      "message",
			Version:   1,
			Payload:   json.RawMessage(`{"text":"first"}`),
		},
		entry.Entry{
			SessionID: "session-1",
			Type:      "message",
			Version:   1,
			Payload:   json.RawMessage(`{"text":"second"}`),
		},
		entry.Entry{
			SessionID: "session-2",
			Type:      "message",
			Version:   1,
			Payload:   json.RawMessage(`{"text":"other"}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session := New(State{ID: "session-1"}, store, nil)

	entries, err := session.Entries(context.Background(), 1)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Entries() returned %d entries, want 1", len(entries))
	}

	if got, want := entries[0].ID, uint64(2); got != want {
		t.Fatalf("entry ID = %d, want %d", got, want)
	}
}
