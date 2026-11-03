package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestSessionID(t *testing.T) {
	t.Parallel()

	session := newSession(context.Background(), t, "session-1", entry.NewMemoryStore(), execution.NewRegistry())

	if got, want := session.ID(), "session-1"; got != want {
		t.Fatalf("ID() = %q, want %q", got, want)
	}
}

func TestSessionEntries(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(context.Background(), "session-1",
		entry.Entry{
			Type:    "message",
			Payload: json.RawMessage(`{"text":"first"}`),
		},
		entry.Entry{
			Type:    "message",
			Payload: json.RawMessage(`{"text":"second"}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	_, err = store.Append(context.Background(), "session-2", entry.Entry{
		Type:    "message",
		Payload: json.RawMessage(`{"text":"other"}`),
	})
	if err != nil {
		t.Fatalf("Append() other session error = %v", err)
	}

	session := newSession(context.Background(), t, "session-1", store, execution.NewRegistry())

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

func TestSubscribeReplaysEntries(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(context.Background(), "session-1", entry.Entry{
		Type:    "test.entry",
		Payload: json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session := newSession(context.Background(), t, "session-1", store, execution.NewRegistry())

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	event := receiveEvent(t, subscription)
	if event.EntryID != 1 {
		t.Fatalf("replayed event = %#v, want entry 1", event)
	}
}

func TestEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()

	session := newSession(context.Background(), t, "session-1", entry.NewMemoryStore(), execution.NewRegistry())

	first, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("first Subscribe() error = %v", err)
	}

	t.Cleanup(first.Close)

	second, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}

	t.Cleanup(second.Close)

	if err := session.Emit(
		context.Background(),
		"execution-1",
		"test.event",
		map[string]int{"value": 1},
	); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	for _, subscription := range []*event.Subscription{first, second} {
		received := receiveEvent(t, subscription)
		if received.SessionID != "session-1" {
			t.Fatalf("SessionID = %q, want %q", received.SessionID, "session-1")
		}

		if received.ExecutionID != "execution-1" {
			t.Fatalf("ExecutionID = %q, want %q", received.ExecutionID, "execution-1")
		}

		if received.EntryID != 0 || received.Type != "test.event" {
			t.Fatalf("event = %#v, want transient test.event", received)
		}
	}
}

func TestExecutePublishesEntriesAndEvents(t *testing.T) {
	t.Parallel()

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		if err := params.Events.Emit(ctx, params.ExecutionID, "work.progress", "halfway"); err != nil {
			return err
		}

		return params.Events.Commit(ctx, params.ExecutionID, "work.output", "done")
	})
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	current, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for i, want := range []struct {
		entryID   uint64
		eventType string
	}{
		{1, execution.StartedEntryType},
		{0, "work.progress"},
		{2, "work.output"},
		{3, execution.FinishedEntryType},
	} {
		received := receiveEvent(t, subscription)
		if received.EntryID != want.entryID || received.Type != want.eventType || received.ExecutionID != current.ID() {
			t.Fatalf("event %d = %#v, want %s with entry ID %d", i, received, want.eventType, want.entryID)
		}
	}
}

func TestNewUsesContextForConstructionAndLifetime(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		cancelled bool
	}{
		{name: "active context"},
		{name: "cancelled context", cancelled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if test.cancelled {
				cancel()
			}

			opened, err := New(ctx, "session-1", newSessionState(t, "session-1"), entry.NewMemoryStore(), execution.NewRegistry())
			if test.cancelled {
				if opened != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled construction = %v, error = %v", opened, err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			defer opened.Close()

			cancel()

			if _, err := opened.Execute(context.Background(), execution.NewRequest("work", nil)); !errors.Is(err, ErrClosed) {
				t.Fatalf("session after context cancelled = %v", err)
			}
		})
	}
}

func TestCloseRejectsExecution(t *testing.T) {
	t.Parallel()

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		t.Error("closed session ran work")

		return nil
	})
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	session.Close()
	session.Close()

	_, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Execute() error = %v, want ErrClosed", err)
	}
}

func TestCloseCancelsActiveExecution(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	current, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-executor.started
	session.Close()

	select {
	case <-subscription.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription to stop")
	}

	if err := subscription.Err(); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("subscription Err() = %v, want event.ErrClosed", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	last := entries[len(entries)-1]
	if last.ExecutionID != current.ID() || last.Type != execution.FinishedEntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, current.ID())
	}

	var finished execution.Finished
	if err := json.Unmarshal(last.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Status != execution.Cancelled {
		t.Fatalf("execution status = %q, want %q", finished.Status, execution.Cancelled)
	}
}

func receiveEvent(t *testing.T, subscription *event.Subscription) event.Event {
	t.Helper()

	select {
	case event, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")

		return event.Event{}
	}
}

func newExecutorSession(ctx context.Context, t *testing.T, id string, entries entry.Store, name string, executor execution.Executor) *Session {
	t.Helper()

	registry := execution.NewRegistry()
	registry.Register(name, executor)

	return newSession(ctx, t, id, entries, registry)
}

func newSession(ctx context.Context, t *testing.T, id string, entries entry.Store, executors execution.Resolver) *Session {
	t.Helper()

	opened, err := New(ctx, id, newSessionState(t, id), entries, executors)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(opened.Close)

	return opened
}

func newSessionState(t *testing.T, id string) state.State {
	t.Helper()

	store := state.NewMemoryStore()
	if err := store.Save(context.Background(), state.Record{ID: id}); err != nil {
		t.Fatal(err)
	}

	return state.New(store, id)
}
