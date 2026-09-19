package entry

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestMemoryStoreAppendAndList(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	input := []Entry{
		{
			ExecutionID: "execution-1",
			Type:        "user.message",
			Payload:     json.RawMessage(`{"text":"hello"}`),
		},
		{
			ExecutionID: "execution-1",
			Type:        "assistant.message",
			Payload:     json.RawMessage(`{"text":"hi"}`),
		},
	}

	appended, err := store.Append(context.Background(), "session-1", input...)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if len(appended) != 2 {
		t.Fatalf("Append() returned %d entries, want 2", len(appended))
	}

	for i, entry := range appended {
		if entry.SessionID != "session-1" {
			t.Errorf("entry %d SessionID = %q, want %q", i, entry.SessionID, "session-1")
		}

		if entry.ID != uint64(i+1) {
			t.Errorf("entry %d ID = %d, want %d", i, entry.ID, i+1)
		}

		if entry.CreatedAt.IsZero() {
			t.Errorf("entry %d CreatedAt is zero", i)
		}
	}

	listed, err := store.List(context.Background(), "session-1", 1)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 1 || listed[0].ID != 2 {
		t.Fatalf("List(after 1) = %#v, want ID 2", listed)
	}
}

func TestMemoryStoreKeepsSessionsSeparate(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	entry := Entry{Type: "checkpoint", Payload: json.RawMessage(`{}`)}
	if _, err := store.Append(context.Background(), "session-1", entry); err != nil {
		t.Fatalf("Append(session-1) error = %v", err)
	}

	if _, err := store.Append(context.Background(), "session-2", entry); err != nil {
		t.Fatalf("Append(session-2) error = %v", err)
	}

	for _, sessionID := range []string{"session-1", "session-2"} {
		entries, err := store.List(context.Background(), sessionID, 0)
		if err != nil {
			t.Fatalf("List(%s) error = %v", sessionID, err)
		}

		if len(entries) != 1 {
			t.Fatalf("List(%s) returned %d entries, want 1", sessionID, len(entries))
		}

		if entries[0].ID != 1 {
			t.Errorf("List(%s) ID = %d, want 1", sessionID, entries[0].ID)
		}
	}
}

func TestMemoryStoreOwnsPayloads(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	payload := json.RawMessage(`{"value":1}`)

	appended, err := store.Append(context.Background(), "session-1", Entry{
		Type:    "value.changed",
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	payload[0] = '['
	appended[0].Payload[0] = '['

	listed, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if got, want := string(listed[0].Payload), `{"value":1}`; got != want {
		t.Fatalf("stored Payload = %s, want %s", got, want)
	}

	listed[0].Payload[0] = '['

	listedAgain, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("second List() error = %v", err)
	}

	if got, want := string(listedAgain[0].Payload), `{"value":1}`; got != want {
		t.Fatalf("stored Payload after List mutation = %s, want %s", got, want)
	}
}

func TestMemoryStoreHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Append(ctx, "session-1", Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append() error = %v, want context.Canceled", err)
	}

	if _, err := store.List(ctx, "session-1", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}
}

func TestMemoryStoreConcurrentAppend(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	const count = 100

	var wg sync.WaitGroup
	for range count {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Append(context.Background(), "session-1", Entry{
				Type:    "test.entry",
				Payload: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Errorf("Append() error = %v", err)
			}
		}()
	}

	wg.Wait()

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(entries) != count {
		t.Fatalf("List() returned %d entries, want %d", len(entries), count)
	}

	for i, entry := range entries {
		if entry.ID != uint64(i+1) {
			t.Fatalf("entry %d ID = %d, want %d", i, entry.ID, i+1)
		}
	}
}
