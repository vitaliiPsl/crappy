package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestNewSettlesAbandonedExecutionBeforeReturning(t *testing.T) {
	t.Parallel()

	store := newFinishFailingStore(errors.New("storage unavailable"))

	bus := event.NewBus("session-1", store)
	if err := bus.Commit(context.Background(), "unfinished", execution.EntryType, execution.Record{
		ID: "unfinished", Executor: "removed-executor", Input: json.RawMessage(`{"value":1}`), Status: execution.Running,
	}); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		calls.Add(1)

		entries, err := params.Entries.List(ctx, params.SessionID, 0)
		if err != nil {
			return err
		}

		if len(entries) != 4 || entries[1].ExecutionID != "unfinished" {
			t.Errorf("new work started before recovery: %+v", entries)
		}

		return nil
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

	current, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 5 {
		t.Fatalf("recovered journal = %+v, %v", entries, err)
	}

	var previous execution.Record
	if err := json.Unmarshal(entries[1].Payload, &previous); err != nil {
		t.Fatal(err)
	}

	if previous.Status != execution.Interrupted || previous.Executor != "removed-executor" || string(previous.Input) != `{"value":1}` || calls.Load() != 1 {
		t.Fatalf("previous execution = %+v, calls = %d", previous, calls.Load())
	}
}

func TestNewPreservesCompletedExecution(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	bus := event.NewBus("session-1", store)
	if err := bus.Commit(context.Background(), "completed", execution.EntryType, execution.Record{ID: "completed", Status: execution.Running}); err != nil {
		t.Fatal(err)
	}

	if err := bus.Commit(context.Background(), "completed", execution.EntryType, execution.Record{ID: "completed", Status: execution.Completed}); err != nil {
		t.Fatal(err)
	}

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error { return nil })
	session := newExecutorSession(context.Background(), t, "session-1", store, "work", executor)

	current, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 5 {
		t.Fatalf("completed execution changed: %+v, %v", entries, err)
	}
}

func TestNewInterruptsQueuedExecutions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := entry.NewMemoryStore()

	bus := event.NewBus("session", store)
	for _, id := range []string{"running", "second", "third"} {
		if err := bus.Commit(ctx, id, execution.EntryType, execution.Record{
			ID: id, Executor: "work", Input: json.RawMessage(`"` + id + `"`), Status: execution.Queued,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := bus.Commit(ctx, "running", execution.EntryType, execution.Record{
		ID: "running", Executor: "work", Input: json.RawMessage(`"running"`), Status: execution.Running,
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan string, 4)
	executor := execution.Func(func(_ context.Context, params execution.ExecuteParams) error {
		var input string
		if err := json.Unmarshal(params.Input, &input); err != nil {
			return err
		}

		started <- input

		return nil
	})

	for range 2 {
		opened := newExecutorSession(ctx, t, "session", store, "work", executor)

		for _, id := range []string{"running", "second", "third"} {
			interrupted, err := opened.Execution(ctx, id)
			if err != nil {
				t.Fatal(err)
			}

			if status, err := interrupted.Status(); err != nil || status != execution.Interrupted {
				t.Fatalf("execution %s after restart = %q, %v", id, status, err)
			}
		}

		opened.Close()
	}

	entries, err := store.List(ctx, "session", 0)
	if err != nil || len(entries) != 7 {
		t.Fatalf("repeated recovery changed the journal: %+v, %v", entries, err)
	}

	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	current, err := opened.Execute(ctx, execution.NewRequest("work", "new"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("new execution after recovery = %q, %v", status, err)
	}

	if input := receiveStarted(ctx, t, started); input != "new" {
		t.Fatalf("recovered work ran: %s", input)
	}
}
