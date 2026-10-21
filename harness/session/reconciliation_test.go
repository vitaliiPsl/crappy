package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestNewReconcilesUnfinishedExecution(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(
		context.Background(),
		"session-1",
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        execution.StartedEntryType,
			Payload:     json.RawMessage(`{}`),
		},
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        "message",
			Payload:     json.RawMessage(`{}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		store,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if got, want := len(entries), 3; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}

	finishedEntry := entries[2]
	if finishedEntry.ExecutionID != "execution-1" ||
		finishedEntry.Type != execution.FinishedEntryType {
		t.Fatalf("finished entry = %#v", finishedEntry)
	}

	var finished execution.Finished
	if err := json.Unmarshal(finishedEntry.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, execution.Interrupted; got != want {
		t.Fatalf("execution outcome = %q, want %q", got, want)
	}
}

func TestNewDoesNotReconcileFinishedExecution(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(
		context.Background(),
		"session-1",
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        execution.StartedEntryType,
			Payload:     json.RawMessage(`{}`),
		},
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        execution.FinishedEntryType,
			Payload:     json.RawMessage(`{"outcome":"completed"}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		store,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if got, want := len(entries), 2; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}
}
