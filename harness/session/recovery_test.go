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

	broker := event.NewBroker("session-1", store)
	if err := broker.Commit(context.Background(), "unfinished", execution.EntryType, execution.Record{
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

		if len(entries) != 5 || entries[1].ExecutionID != "unfinished" {
			t.Errorf("new work started before recovery: %+v", entries)
		}

		return nil
	})

	store.failing.Store(true)

	for range 2 {
		opened, err := New(context.Background(), "session-1", newSessionState(t, "session-1"), store, event.NewBroker("session-1", store), execution.NewRegistry())
		if opened != nil || !errors.Is(err, store.err) {
			t.Fatalf("failed construction = %v, error = %v", opened, err)
		}
	}

	if calls.Load() != 0 {
		t.Fatal("constructor invoked an executor")
	}

	store.failing.Store(false)

	session := newExecutorSession(context.Background(), t, "session-1", store, "work", executor)
	if resumed, err := session.Resume(context.Background()); resumed != nil || err == nil {
		t.Fatalf("resume with a missing executor = %v, %v", resumed, err)
	}

	if err := session.Cancel(context.Background(), "unfinished"); err != nil {
		t.Fatal(err)
	}

	current, err := session.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 6 {
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

	broker := event.NewBroker("session-1", store)
	if err := broker.Commit(context.Background(), "completed", execution.EntryType, execution.Record{ID: "completed", Status: execution.Running}); err != nil {
		t.Fatal(err)
	}

	if err := broker.Commit(context.Background(), "completed", execution.EntryType, execution.Record{ID: "completed", Status: execution.Completed}); err != nil {
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

func TestNewRestoresQueueUntilExplicitResume(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := entry.NewMemoryStore()

	broker := event.NewBroker("session", store)
	for _, id := range []string{"running", "second", "third"} {
		if err := broker.Commit(ctx, id, execution.EntryType, execution.Record{
			ID: id, Executor: "work", Input: json.RawMessage(`"` + id + `"`), Status: execution.Queued,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := broker.Commit(ctx, "running", execution.EntryType, execution.Record{
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

		var restored []*Execution

		for i, id := range []string{"running", "second", "third"} {
			pending, err := opened.Execution(ctx, id)
			if err != nil {
				t.Fatal(err)
			}

			want := execution.Queued
			if i == 0 {
				want = execution.Interrupted
			}

			if status, err := pending.Status(); err != nil || status != want {
				t.Fatalf("execution %s after restart = %q, %v", id, status, err)
			}

			restored = append(restored, pending)
		}

		opened.Close()

		for _, pending := range restored {
			if _, err := pending.Wait(ctx); !errors.Is(err, ErrClosed) {
				t.Fatalf("closed runtime did not release execution %s: %v", pending.ID(), err)
			}
		}
	}

	entries, err := store.List(ctx, "session", 0)
	if err != nil || len(entries) != 5 {
		t.Fatalf("repeated recovery changed the journal: %+v, %v", entries, err)
	}

	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	interrupted, err := opened.Execution(ctx, "running")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-interrupted.Done():
		t.Fatal("restored interrupted execution is already done")
	default:
	}

	waited := make(chan error, 1)
	go func() {
		status, err := interrupted.Wait(ctx)
		if err == nil && status != execution.Completed {
			err = errors.New("restored execution waiter did not observe completion")
		}

		waited <- err
	}()

	current, err := opened.Execute(ctx, execution.NewRequest("work", "new"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := current.Status(); err != nil || status != execution.Queued {
		t.Fatalf("new execution bypassed restored work = %q, %v", status, err)
	}

	select {
	case input := <-started:
		t.Fatalf("work started before resume: %s", input)
	default:
	}

	resumed, err := opened.Resume(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if resumed != interrupted {
		t.Fatal("resume replaced the restored execution handle")
	}

	for _, id := range []string{"running", "second", "third", current.ID()} {
		pending, err := opened.Execution(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		if status, err := pending.Wait(ctx); err != nil || status != execution.Completed {
			t.Fatalf("resumed execution %s = %q, %v", id, status, err)
		}
	}

	for _, want := range []string{"running", "second", "third", "new"} {
		if input := receiveStarted(ctx, t, started); input != want {
			t.Fatalf("execution order = %s, want %s", input, want)
		}
	}

	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("restored execution waiter did not finish after resume")
	}
}

func TestNewRetriesPartialRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	entries := entry.NewMemoryStore()
	broker := event.NewBroker("session", entries)

	records := []execution.Record{
		{ID: "first", Executor: "work", Input: json.RawMessage(`{"value":1}`), Status: execution.Running},
		{ID: "second", Executor: "work", Input: json.RawMessage(`{"value":2}`), Status: execution.Running},
	}
	for _, record := range records {
		if err := broker.Commit(ctx, record.ID, execution.EntryType, record); err != nil {
			t.Fatal(err)
		}
	}

	failure := errors.New("storage unavailable")
	store := newRecoveryFailingStore(entries, 2, failure)

	events := event.NewBroker("session", store)

	opened, err := New(ctx, "session", newSessionState(t, "session"), store, events, execution.NewRegistry())
	if opened != nil || !errors.Is(err, failure) {
		t.Fatalf("partial recovery = %v, %v", opened, err)
	}

	if _, err := events.Subscribe(ctx, 0); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("failed restoration left event broker open: %v", err)
	}

	partial, err := entries.List(ctx, "session", 0)
	if err != nil || len(partial) != 3 || partial[2].ExecutionID != "first" {
		t.Fatalf("partial recovery journal = %+v, %v", partial, err)
	}

	for range 2 {
		session := newSession(ctx, t, "session", store, execution.NewRegistry())
		session.Close()
	}

	recovered, err := entries.List(ctx, "session", 0)
	if err != nil || len(recovered) != 4 {
		t.Fatalf("repeated recovery journal = %+v, %v", recovered, err)
	}

	for i, original := range records {
		var record execution.Record
		if err := json.Unmarshal(recovered[i+2].Payload, &record); err != nil {
			t.Fatal(err)
		}

		if record.ID != original.ID || record.Executor != original.Executor || string(record.Input) != string(original.Input) || record.Status != execution.Interrupted || record.Error != "" {
			t.Fatalf("recovered execution = %+v, original = %+v", record, original)
		}
	}
}

type recoveryFailingStore struct {
	entry.Store
	failOn  int
	appends int
	err     error
}

func newRecoveryFailingStore(store entry.Store, failOn int, err error) *recoveryFailingStore {
	return &recoveryFailingStore{Store: store, failOn: failOn, err: err}
}

func (s *recoveryFailingStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	s.appends++
	if s.appends == s.failOn {
		return nil, s.err
	}

	return s.Store.Append(ctx, sessionID, entries...)
}
