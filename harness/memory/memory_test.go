package memory

import (
	"context"
	"reflect"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

func TestRecordAndHistory(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()
	memory := New("session-1", "execution-1", store, event.NewBus("session-1", store))
	messages := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("hello")),
		kit.NewModelMessage(kit.NewTextContent("hi")),
	}

	if err := memory.Record(context.Background(), messages...); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	history, err := memory.History(context.Background())
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if !reflect.DeepEqual(history, messages) {
		t.Fatalf("History() = %#v, want %#v", history, messages)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	for _, stored := range entries {
		if stored.ExecutionID != "execution-1" {
			t.Fatalf("ExecutionID = %q, want %q", stored.ExecutionID, "execution-1")
		}

		if stored.Type != messageEntryType {
			t.Fatalf("entry type = %q, want %q", stored.Type, messageEntryType)
		}
	}
}

func TestContextStartsAtLatestCheckpoint(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	memory := New("session-1", "execution-1", store, bus)

	if err := memory.Record(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("old")),
	); err != nil {
		t.Fatalf("Record(old) error = %v", err)
	}

	if err := bus.Commit(
		context.Background(),
		"",
		checkpoint.EntryType,
		checkpoint.Checkpoint{Summary: "summary"},
	); err != nil {
		t.Fatalf("Commit(checkpoint) error = %v", err)
	}

	if err := memory.Record(
		context.Background(),
		kit.NewModelMessage(kit.NewTextContent("new")),
	); err != nil {
		t.Fatalf("Record(new) error = %v", err)
	}

	contextMessages, err := memory.Context(context.Background())
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}

	want := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("summary")),
		kit.NewModelMessage(kit.NewTextContent("new")),
	}
	if !reflect.DeepEqual(contextMessages, want) {
		t.Fatalf("Context() = %#v, want %#v", contextMessages, want)
	}
}
