package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestNewSettlesAbandonedExecutionBeforeReturning(t *testing.T) {
	t.Parallel()

	store := newFinishFailingStore(errors.New("storage unavailable"))

	bus := event.NewBus("session-1", store)
	if err := bus.Commit(context.Background(), "unfinished", execution.StartedEntryType, execution.Started{
		Executor: "removed-executor", Input: json.RawMessage(`{"value":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) (any, error) {
		calls.Add(1)

		entries, err := params.Entries.List(ctx, params.SessionID, 0)
		if err != nil {
			return nil, err
		}

		if len(entries) != 3 || entries[1].Type != execution.FinishedEntryType {
			t.Errorf("new work started before recovery: %+v", entries)
		}

		return nil, nil
	})

	store.failing.Store(true)

	for range 2 {
		opened, err := New(context.Background(), "session-1", newSessionState(t, "session-1"), store, execution.NewRegistry())
		if opened != nil || !errors.Is(err, store.err) {
			t.Fatalf("failed construction = %v, error = %v", opened, err)
		}
	}

	if calls.Load() != 0 {
		t.Fatal("constructor invoked an executor")
	}

	store.failing.Store(false)

	session := newExecutorSession(context.Background(), t, "session-1", store, "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("recovered journal = %+v, %v", entries, err)
	}

	var previous execution.Result
	if err := json.Unmarshal(entries[1].Payload, &previous); err != nil {
		t.Fatal(err)
	}

	if previous.Outcome != execution.Interrupted || calls.Load() != 1 {
		t.Fatalf("previous outcome = %+v, calls = %d", previous, calls.Load())
	}
}

func TestNewPreservesCompletedExecution(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	bus := event.NewBus("session-1", store)
	if err := bus.Commit(context.Background(), "completed", execution.StartedEntryType, execution.Started{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	if err := bus.Commit(context.Background(), "completed", execution.FinishedEntryType, execution.Result{Outcome: execution.Completed}); err != nil {
		t.Fatal(err)
	}

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) { return nil, nil })
	session := newExecutorSession(context.Background(), t, "session-1", store, "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("completed execution changed: %+v, %v", entries, err)
	}
}
