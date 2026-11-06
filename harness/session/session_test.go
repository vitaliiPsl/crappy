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

	for _, subscription := range []event.Subscription{first, second} {
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
		{1, execution.EntryType},
		{2, execution.EntryType},
		{0, "work.progress"},
		{3, "work.output"},
		{4, execution.EntryType},
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

			entries := entry.NewMemoryStore()
			events := event.NewBroker("session-1", entries)

			opened, err := New(ctx, "session-1", newSessionState(t, "session-1"), entries, events, execution.NewRegistry())
			if test.cancelled {
				if opened != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled construction = %v, error = %v", opened, err)
				}

				if _, err := events.Subscribe(context.Background(), 0); !errors.Is(err, event.ErrClosed) {
					t.Fatalf("failed construction left event broker open: %v", err)
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

func TestNewUsesInjectedEventBroker(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries := entry.NewMemoryStore()
	events := newWrappedEventBroker("session", entries)
	executors := execution.NewRegistry()
	executors.Register("work", execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		if params.Events != events {
			return errors.New("executor received a different publisher")
		}

		if err := params.Events.Emit(ctx, params.ExecutionID, "work.progress", "working"); err != nil {
			return err
		}

		return params.Events.Commit(ctx, params.ExecutionID, "work.output", "done")
	}))

	opened, err := New(ctx, "session", newSessionState(t, "session"), entries, events, executors)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	if _, ok := subscription.(*wrappedSubscription); !ok {
		t.Fatal("session replaced the custom subscription")
	}

	current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("execution = %q, %v", status, err)
	}

	for _, want := range []string{execution.EntryType, execution.EntryType, "work.progress", "work.output", execution.EntryType} {
		value := receiveEvent(t, subscription)
		if value.Type != want || value.SessionID != opened.ID() || value.ExecutionID != current.ID() {
			t.Fatalf("event = %+v, want %s for current execution", value, want)
		}
	}

	stored, err := opened.Entries(ctx, 0)
	if err != nil || len(stored) != 4 || stored[2].Type != "work.output" {
		t.Fatalf("broker commits and session reads used different journals: %+v, %v", stored, err)
	}

	opened.Close()

	if _, err := events.Subscribe(ctx, 0); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("session left injected broker open: %v", err)
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

func TestCloseReleasesQueuedExecutions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := entry.NewMemoryStore()
	executor := newBlockingExecutor()
	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	running, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	<-executor.started

	queued, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	opened.Close()

	if status, err := running.Wait(ctx); err != nil || status != execution.Interrupted {
		t.Fatalf("running execution after close = %q, %v", status, err)
	}

	if _, err := queued.Wait(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("queued execution waiter = %v", err)
	}

	if _, err := opened.Execute(ctx, execution.NewRequest("work", nil)); !errors.Is(err, ErrClosed) {
		t.Fatalf("submit to closed session = %v", err)
	}

	entries, err := store.List(ctx, "session", 0)
	if err != nil {
		t.Fatal(err)
	}

	var saved execution.Record
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &saved); err != nil {
		t.Fatal(err)
	}

	if saved.ID != running.ID() || saved.Status != execution.Interrupted {
		t.Fatalf("closing settled queued work: %+v", saved)
	}
}

func TestCloseInterruptsActiveExecution(t *testing.T) {
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
	if last.ExecutionID != current.ID() || last.Type != execution.EntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, current.ID())
	}

	var finished execution.Record
	if err := json.Unmarshal(last.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Status != execution.Interrupted {
		t.Fatalf("execution status = %q, want %q", finished.Status, execution.Interrupted)
	}
}

func receiveEvent(t *testing.T, subscription event.Subscription) event.Event {
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

	opened, err := New(ctx, id, newSessionState(t, id), entries, event.NewBroker(id, entries), executors)
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

type wrappedEventBroker struct {
	event.Broker
}

func newWrappedEventBroker(sessionID string, entries entry.Store) *wrappedEventBroker {
	return &wrappedEventBroker{Broker: event.NewBroker(sessionID, entries)}
}

func (b *wrappedEventBroker) Subscribe(ctx context.Context, afterID uint64) (event.Subscription, error) {
	subscription, err := b.Broker.Subscribe(ctx, afterID)
	if err != nil {
		return nil, err
	}

	return newWrappedSubscription(subscription), nil
}

type wrappedSubscription struct {
	event.Subscription
}

func newWrappedSubscription(subscription event.Subscription) *wrappedSubscription {
	return &wrappedSubscription{Subscription: subscription}
}
