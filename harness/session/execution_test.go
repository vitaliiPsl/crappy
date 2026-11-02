package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestExecuteRecordsEachExecutionSeparately(t *testing.T) {
	t.Parallel()

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		return params.Events.Commit(ctx, params.ExecutionID, "work.output", params.Input)
	})
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	var ids []string

	for _, input := range []string{"first", "second"} {
		current, err := session.Execute(context.Background(), execution.Request{Executor: "work", Input: input})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		waitExecution(t, current)

		ids = append(ids, current.ID())
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 6 {
		t.Fatalf("Entries() = %+v, error = %v, want three per execution", entries, err)
	}

	wantTypes := []string{execution.StartedEntryType, "work.output", execution.FinishedEntryType}
	for i, value := range entries {
		if value.Type != wantTypes[i%3] || value.ExecutionID != ids[i/3] {
			t.Fatalf("entry %d = %+v, want %s from %s", i, value, wantTypes[i%3], ids[i/3])
		}
	}
}

func TestExecuteRejectsConcurrentExecution(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if len(entries) == 0 ||
		entries[0].ExecutionID != current.ID() ||
		entries[0].Type != execution.StartedEntryType {
		t.Fatalf("first entry = %#v, want started execution %q", entries, current.ID())
	}

	<-executor.started

	_, err = session.Execute(context.Background(), execution.Request{Executor: "work"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Execute() error = %v, want ErrBusy", err)
	}

	close(executor.release)
	waitExecution(t, current)
}

func TestExecuteRecordsFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("work failed")
	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		return wantErr
	})
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	waitExecution(t, current)

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

	if got, want := finished.Status, execution.Failed; got != want {
		t.Fatalf("execution status = %q, want %q", got, want)
	}

	if got, want := finished.Error, wantErr.Error(); got != want {
		t.Fatalf("execution error = %q, want %q", got, want)
	}
}

func TestCancelActiveExecution(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-executor.started

	if err := session.Cancel(context.Background(), "other-execution"); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(other) error = %v, want ErrExecutionNotActive", err)
	}

	if err := session.Cancel(context.Background(), current.ID()); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Finished
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Status, execution.Cancelled; got != want {
		t.Fatalf("execution status = %q, want %q", got, want)
	}

	if err := session.Cancel(context.Background(), current.ID()); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(completed) error = %v, want ErrExecutionNotActive", err)
	}
}

func TestSessionLifetimeCancelsActiveExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	executor := newBlockingExecutor()
	session := newExecutorSession(ctx, t, "session-1", entry.NewMemoryStore(), "work", executor)

	current, err := session.Execute(context.Background(), execution.Request{Executor: "work"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-executor.started
	cancel()
	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Finished
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Status != execution.Cancelled {
		t.Fatalf("execution status = %q, want %q", finished.Status, execution.Cancelled)
	}
}

func TestFunctionExecutionUsesSessionLifetimeAndDetachedInput(t *testing.T) {
	t.Parallel()

	started := make(chan execution.ExecuteParams, 1)
	release := make(chan struct{})
	store := entry.NewMemoryStore()

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		started <- params

		<-release

		return params.Events.Commit(ctx, params.ExecutionID, "function.output", params.Input)
	})

	session := newExecutorSession(context.Background(), t, "session-1", store, "function", executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := map[string]int{"value": 1}

	current, err := session.Execute(ctx, execution.Request{Executor: "function", Input: input})
	if err != nil {
		t.Fatal(err)
	}

	params := <-started
	if params.SessionID != session.ID() || params.ExecutionID != current.ID() || params.Entries != store {
		t.Fatalf("executor parameters = %+v", params)
	}

	if current.ID() == "" {
		t.Fatal("submission returned an empty execution ID")
	}

	input["value"] = 2

	cancel()
	close(release)
	waitExecution(t, current)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 || string(entries[1].Payload) != `{"value":1}` {
		t.Fatalf("function journal = %+v", entries)
	}

	var startedEntry execution.Started
	if err := json.Unmarshal(entries[0].Payload, &startedEntry); err != nil {
		t.Fatal(err)
	}

	if string(startedEntry.Input) != `{"value":1}` || startedEntry.StartedAt.IsZero() {
		t.Fatalf("mutated request = %+v", startedEntry)
	}

	var finished execution.Finished
	if err := json.Unmarshal(entries[2].Payload, &finished); err != nil {
		t.Fatal(err)
	}

	if finished.Status != execution.Completed {
		t.Fatalf("client cancellation affected invocation: %+v", finished)
	}
}

func TestExecuteValidatesBeforeRecording(t *testing.T) {
	t.Parallel()

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		t.Error("invalid request reached executor")

		return nil
	})

	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

	for _, request := range []execution.Request{
		{},
		{Executor: "missing"},
		{Executor: "work", Input: json.RawMessage(`invalid`)},
		{Executor: "work", Input: make(chan int)},
	} {
		if _, err := session.Execute(context.Background(), request); err == nil {
			t.Fatal("invalid request was accepted")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := session.Execute(ctx, execution.Request{Executor: "work"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled submission = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid request persisted: %+v, %v", entries, err)
	}
}

func TestExecutionWaitReturnsDurableFinishToIndependentClients(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	release := make(chan struct{})
	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	session := newExecutorSession(ctx, t, "session-1", entry.NewMemoryStore(), "work", executor)
	defer session.Close()

	current, err := session.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	attached, err := session.Execution(ctx, current.ID())
	if err != nil || attached != current {
		t.Fatalf("live attachment = %v, %v", attached, err)
	}

	clientCtx, disconnect := context.WithCancel(ctx)
	disconnect()

	if _, err := current.Wait(clientCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v", err)
	}

	select {
	case <-current.Done():
		t.Fatal("client disconnection completed the execution")
	default:
	}

	var clients sync.WaitGroup
	for range 16 {
		clients.Go(func() {
			if finished, err := attached.Wait(ctx); err != nil || finished.Status != execution.Completed {
				t.Errorf("client finish = %+v, %v", finished, err)
			}
		})
	}

	close(release)
	clients.Wait()

	finished, err := current.Wait(ctx)
	if err != nil || finished.EndedAt.IsZero() {
		t.Fatalf("finish = %+v, %v", finished, err)
	}

	entries, err := session.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var stored execution.Finished
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &stored); err != nil {
		t.Fatal(err)
	}

	if stored.Status != finished.Status || !stored.EndedAt.Equal(finished.EndedAt) {
		t.Fatalf("returned finish differs from persisted finish: %+v, %+v", finished, stored)
	}
}

func TestExecutionFinishSeparatesWorkFailuresFromWaitErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		err     error
		status  execution.Status
		failure string
	}{
		{name: "completed", status: execution.Completed},
		{name: "failed", err: errors.New("work failed"), status: execution.Failed, failure: "work failed"},
		{name: "cancelled", err: context.Canceled, status: execution.Cancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
				return test.err
			})

			session := newExecutorSession(ctx, t, "session-1", entry.NewMemoryStore(), "work", executor)
			defer session.Close()

			current, err := session.Execute(ctx, execution.Request{Executor: "work"})
			if err != nil {
				t.Fatal(err)
			}

			finished, err := current.Wait(ctx)
			if err != nil || finished.Status != test.status || !strings.Contains(finished.Error, test.failure) {
				t.Fatalf("execution finish = %+v, wait error = %v", finished, err)
			}
		})
	}
}

func TestExecutionLookupDoesNotRetryPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newFinishFailingStore(errors.New("storage unavailable"))
	store.failing.Store(true)

	var calls atomic.Int32

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		calls.Add(1)

		return errors.New("work failed")
	})

	session := newExecutorSession(ctx, t, "session-1", store, "work", executor)
	defer session.Close()

	current, err := session.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, store.err) {
		t.Fatalf("failed persistence was not observable: %v", err)
	}

	if err := session.Cancel(ctx, current.ID()); !errors.Is(err, ErrClosed) {
		t.Fatalf("finished work remained cancellable: %v", err)
	}

	store.failing.Store(false)

	for range 3 {
		if _, err := session.Execution(ctx, current.ID()); !errors.Is(err, ErrExecutionUnfinished) {
			t.Fatalf("uncommitted execution lookup = %v", err)
		}

		if _, err := current.Wait(ctx); !errors.Is(err, store.err) {
			t.Fatalf("lookup changed persistence error = %v", err)
		}
	}

	if store.finishes.Load() != 1 {
		t.Fatal("lookup retried persistence")
	}

	if next, err := session.Execute(ctx, execution.Request{Executor: "work"}); next != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("admission after persistence failure = %v, error = %v", next, err)
	}

	if calls.Load() != 1 || store.finishes.Load() != 1 {
		t.Fatal("observation or admission retried failed settlement")
	}
}

func TestExecutionDoneWaitsForPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := &blockingFinishStore{
		Store:   entry.NewMemoryStore(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	release := sync.OnceFunc(func() { close(store.release) })

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		return nil
	})

	session := newExecutorSession(ctx, t, "session-1", store, "work", executor)
	defer session.Close()
	defer release()

	current, err := session.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-store.started:
	case <-ctx.Done():
		t.Fatal("result persistence was not attempted")
	}

	select {
	case <-current.Done():
		t.Fatal("execution became ready before persistence completed")
	default:
	}

	waitCtx, stopWaiting := context.WithCancel(ctx)
	stopWaiting()

	if _, err := current.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting for persistence ignored context: %v", err)
	}

	release()

	if result, err := current.Wait(ctx); err != nil || result.Status != execution.Completed {
		t.Fatalf("durable completion = %+v, %v", result, err)
	}
}

func TestExecutionLookupLeavesUnfinishedHistoryUntouched(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	session := newSession(ctx, t, "session-1", store, execution.NewRegistry())
	defer session.Close()

	if _, err := store.Append(ctx, "session-1",
		entry.Entry{ExecutionID: "old", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`)},
		entry.Entry{ExecutionID: "old", Type: execution.FinishedEntryType, Payload: json.RawMessage(`{"status":"completed"}`)},
		entry.Entry{ExecutionID: "unfinished", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`)},
	); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if _, err := session.Execution(ctx, "missing"); !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("unknown execution = %v", err)
		}

		if _, err := session.Execution(ctx, "unfinished"); !errors.Is(err, ErrExecutionUnfinished) {
			t.Fatalf("unfinished execution = %v", err)
		}

		old, err := session.Execution(ctx, "old")
		if err != nil {
			t.Fatal(err)
		}

		finished, err := old.Wait(ctx)
		if err != nil || finished.Status != execution.Completed {
			t.Fatalf("old execution finish = %+v, %v", finished, err)
		}

		entries, err := session.Entries(ctx, 0)
		if err != nil || len(entries) != 3 {
			t.Fatalf("lookup changed history = %+v, %v", entries, err)
		}
	}
}

func TestExecuteRejectsAfterTerminalPersistenceFailure(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{nil, errors.New("function failed")} {
		t.Run(fmt.Sprint(runErr), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			store := newFinishFailingStore(errors.New("storage unavailable"))
			store.failing.Store(true)

			var calls atomic.Int32

			executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
				calls.Add(1)

				return runErr
			})

			session := newExecutorSession(context.Background(), t, "session-1", store, "work", executor)

			request := execution.Request{Executor: "work"}

			first, err := session.Execute(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := first.Wait(ctx); !errors.Is(err, store.err) {
				t.Fatalf("terminal persistence error = %v", err)
			}

			if err := session.Cancel(context.Background(), first.ID()); !errors.Is(err, ErrClosed) {
				t.Fatalf("cancel after invocation returned = %v", err)
			}

			if _, err := session.Execute(ctx, request); !errors.Is(err, ErrClosed) {
				t.Fatalf("submission after failed commit = %v", err)
			}

			store.failing.Store(false)

			if next, err := session.Execute(ctx, request); next != nil || !errors.Is(err, ErrClosed) {
				t.Fatalf("submission after storage recovered = %v, error = %v", next, err)
			}

			waitExecution(t, first)

			entries, err := session.Entries(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}

			if len(entries) != 1 || calls.Load() != 1 || store.finishes.Load() != 1 {
				t.Fatalf("failed settlement was retried: entries = %+v, calls = %d", entries, calls.Load())
			}
		})
	}
}

func TestExecuteDoesNotReloadExecutionState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newFinishFailingStore(nil)

	session := newExecutorSession(ctx, t, "session-1", store, "work", execution.Func(
		func(context.Context, execution.ExecuteParams) error { return nil },
	))
	defer session.Close()

	reads := store.reads.Load()
	if reads == 0 {
		t.Fatal("initialization did not read persisted execution state")
	}

	for range 3 {
		current, err := session.Execute(ctx, execution.Request{Executor: "work"})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := current.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if got := store.reads.Load(); got != reads {
		t.Fatalf("execution state was reloaded: reads = %d, want %d", got, reads)
	}
}

func TestExecutionRetainsFinishWhenItCannotBeCommitted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := execution.NewRegistry()
	opened := newSession(ctx, t, "session", entry.NewMemoryStore(), registry)

	workErr := errors.New("work failed")
	registry.Register("work", execution.Func(func(context.Context, execution.ExecuteParams) error {
		opened.events.Close()

		return workErr
	}))

	current, err := opened.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("finish error = %v", err)
	}

	original := current.finished

	for range 3 {
		if _, err := opened.Execution(ctx, current.ID()); !errors.Is(err, ErrExecutionUnfinished) {
			t.Fatalf("uncommitted execution lookup = %v", err)
		}

		if _, err := current.Wait(ctx); !errors.Is(err, event.ErrClosed) {
			t.Fatalf("finish error changed = %v", err)
		}
	}

	if retained := current.finished; retained.Status != execution.Failed || retained.Error != workErr.Error() ||
		!retained.EndedAt.Equal(original.EndedAt) {
		t.Fatalf("execution finish changed after failed commit: %+v", retained)
	}
}

func waitExecution(t *testing.T, current *Execution) {
	t.Helper()

	select {
	case <-current.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for execution to finish")
	}
}

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

func newBlockingExecutor() *blockingExecutor {
	return &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
}

func (e *blockingExecutor) Execute(ctx context.Context, _ execution.ExecuteParams) error {
	close(e.started)

	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type finishFailingStore struct {
	entry.Store
	err      error
	failing  atomic.Bool
	finishes atomic.Int32
	reads    atomic.Int32
}

func newFinishFailingStore(err error) *finishFailingStore {
	return &finishFailingStore{Store: entry.NewMemoryStore(), err: err}
}

func (s *finishFailingStore) Latest(ctx context.Context, sessionID, entryType string) (entry.Entry, bool, error) {
	s.reads.Add(1)

	return s.Store.Latest(ctx, sessionID, entryType)
}

func (s *finishFailingStore) Append(
	ctx context.Context,
	sessionID string,
	entries ...entry.Entry,
) ([]entry.Entry, error) {
	for _, current := range entries {
		if current.Type == execution.FinishedEntryType {
			s.finishes.Add(1)

			if s.failing.Load() {
				return nil, s.err
			}
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}

type blockingFinishStore struct {
	entry.Store
	started chan struct{}
	release chan struct{}
}

func (s *blockingFinishStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range entries {
		if value.Type == execution.FinishedEntryType {
			close(s.started)
			<-s.release
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}
