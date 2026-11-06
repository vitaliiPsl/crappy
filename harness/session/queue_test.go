package session

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestQueueAdvancesAfterCancellationCleanupAndPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := make(chan string, 3)
	cleanup := make(chan struct{})

	release := sync.OnceFunc(func() { close(cleanup) })
	defer release()

	store := &blockingFinishStore{Store: entry.NewMemoryStore(), started: make(chan struct{}), release: make(chan struct{})}

	commit := sync.OnceFunc(func() { close(store.release) })
	defer commit()

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		var input string
		if err := json.Unmarshal(params.Input, &input); err != nil {
			return err
		}

		started <- input

		if input == "first" {
			<-ctx.Done()
			<-cleanup

			return ctx.Err()
		}

		return nil
	})
	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	first, err := opened.Execute(ctx, execution.NewRequest("work", "first"))
	if err != nil {
		t.Fatal(err)
	}

	if input := receiveStarted(ctx, t, started); input != "first" {
		t.Fatal(input)
	}

	second, err := opened.Execute(ctx, execution.NewRequest("work", "second"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := second.Status(); err != nil || status != execution.Queued {
		t.Fatalf("pending status = %q, %v", status, err)
	}

	attached, err := opened.Execution(ctx, second.ID())
	if err != nil || attached != second {
		t.Fatalf("queued attachment = %v, %v", attached, err)
	}

	if err := opened.Cancel(ctx, first.ID()); err != nil {
		t.Fatal(err)
	}

	select {
	case input := <-started:
		t.Fatalf("%s overlapped cancellation cleanup", input)
	default:
	}

	release()

	select {
	case <-store.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	select {
	case input := <-started:
		t.Fatalf("%s started before cancellation was durable", input)
	default:
	}

	commit()

	if status, err := first.Wait(ctx); err != nil || status != execution.Cancelled {
		t.Fatalf("cancelled execution = %q, %v", status, err)
	}

	if input := receiveStarted(ctx, t, started); input != "second" {
		t.Fatal(input)
	}

	if status, err := second.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("next execution = %q, %v", status, err)
	}
}

func TestQueuePreservesOrderAndSkipsCancelledRequests(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := make(chan string, 4)
	release := make(chan struct{})
	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		var input string
		if err := json.Unmarshal(params.Input, &input); err != nil {
			return err
		}

		started <- input

		if input == "first" {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}

			return errors.New("first failed")
		}

		return nil
	})
	opened := newExecutorSession(ctx, t, "session", entry.NewMemoryStore(), "work", executor)

	var accepted []*Execution

	for _, input := range []string{"first", "second", "cancelled", "fourth"} {
		current, err := opened.Execute(ctx, execution.NewRequest("work", input))
		if err != nil {
			t.Fatal(err)
		}

		accepted = append(accepted, current)
	}

	if input := receiveStarted(ctx, t, started); input != "first" {
		t.Fatal(input)
	}

	if err := opened.Cancel(ctx, accepted[2].ID()); err != nil {
		t.Fatal(err)
	}

	if status, err := accepted[2].Wait(ctx); err != nil || status != execution.Cancelled {
		t.Fatalf("queued cancellation = %q, %v", status, err)
	}

	close(release)

	for i, want := range []execution.Status{execution.Failed, execution.Completed, execution.Cancelled, execution.Completed} {
		if status, err := accepted[i].Wait(ctx); err != nil || status != want {
			t.Fatalf("execution %d = %q, %v; want %q", i, status, err, want)
		}
	}

	if got := []string{receiveStarted(ctx, t, started), receiveStarted(ctx, t, started)}; !slices.Equal(got, []string{"second", "fourth"}) {
		t.Fatalf("execution order = %v", got)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var active string
	for _, value := range entries {
		if value.Type != execution.EntryType {
			continue
		}

		var saved execution.Record
		if err := json.Unmarshal(value.Payload, &saved); err != nil {
			t.Fatal(err)
		}

		switch saved.Status {
		case execution.Running:
			if active != "" || value.ExecutionID == accepted[2].ID() {
				t.Fatalf("invalid start %s while %s is active", value.ExecutionID, active)
			}

			active = value.ExecutionID
		case execution.Completed, execution.Cancelled, execution.Failed, execution.Interrupted:
			if value.ExecutionID == active {
				active = ""
			}
		}
	}
}

func TestConcurrentSubmissionsFollowDurableQueueOrder(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	release := make(chan struct{})

	var active atomic.Int32

	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) error {
		if active.Add(1) != 1 {
			t.Error("executions overlapped")
		}
		defer active.Add(-1)

		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	opened := newExecutorSession(ctx, t, "session", entry.NewMemoryStore(), "work", executor)
	accepted := make(chan *Execution, 20)

	var clients sync.WaitGroup

	for range cap(accepted) {
		clients.Go(func() {
			current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
			if err != nil {
				t.Error(err)

				return
			}

			accepted <- current
		})
	}

	clients.Wait()
	close(accepted)
	close(release)

	for current := range accepted {
		if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
			t.Fatalf("completion = %q, %v", status, err)
		}
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var queued, started []string
	for _, value := range entries {
		var saved execution.Record
		if err := json.Unmarshal(value.Payload, &saved); err != nil {
			t.Fatal(err)
		}

		switch saved.Status {
		case execution.Queued:
			queued = append(queued, value.ExecutionID)
		case execution.Running:
			started = append(started, value.ExecutionID)
		}
	}

	if len(queued) != 20 || !slices.Equal(queued, started) {
		t.Fatalf("queued = %v, started = %v", queued, started)
	}
}

func TestQueueStopsAfterTerminalPersistenceFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newFinishFailingStore(errors.New("storage unavailable"))
	store.failing.Store(true)

	release := make(chan struct{})

	var calls atomic.Int32

	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) error {
		calls.Add(1)

		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	first, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	second, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	close(release)

	for _, current := range []*Execution{first, second} {
		if _, err := current.Wait(ctx); !errors.Is(err, store.err) {
			t.Fatalf("persistence failure = %v", err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("queue advanced after failed completion: calls = %d", calls.Load())
	}

	store.failing.Store(false)
	restored := newExecutorSession(ctx, t, "session", store, "work", executor)

	for i, current := range []*Execution{first, second} {
		interrupted, err := restored.Execution(ctx, current.ID())
		if err != nil {
			t.Fatal(err)
		}

		want := execution.Queued
		if i == 0 {
			want = execution.Interrupted
		}

		if status, err := interrupted.Status(); err != nil || status != want {
			t.Fatalf("restored execution %s = %q, %v", current.ID(), status, err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("restart ran queued work: calls = %d", calls.Load())
	}
}

func TestQueueAdmissionAndStartRequirePersistence(t *testing.T) {
	t.Parallel()

	for _, status := range []execution.Status{execution.Queued, execution.Running} {
		t.Run(string(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			store := newQueueFailingStore(status)
			store.failing.Store(true)

			var calls atomic.Int32

			executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
				calls.Add(1)

				return nil
			})
			opened := newExecutorSession(ctx, t, "session", store, "work", executor)

			accepted, err := opened.Execute(ctx, execution.NewRequest("work", nil))
			if status == execution.Queued {
				if accepted != nil || !errors.Is(err, store.err) {
					t.Fatalf("admission = %v, %v", accepted, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}

				if _, err := accepted.Wait(ctx); !errors.Is(err, store.err) {
					t.Fatalf("start persistence error = %v", err)
				}
			}

			if calls.Load() != 0 {
				t.Fatal("executor ran before durable admission and start")
			}

			opened.Close()
			store.failing.Store(false)

			restored := newExecutorSession(ctx, t, "session", store, "work", executor)
			if status == execution.Queued {
				if restored.current != nil || len(restored.queue) != 0 {
					t.Fatal("failed admission created queued work")
				}

				return
			}

			interrupted, err := restored.Execution(ctx, accepted.ID())
			if err != nil {
				t.Fatal(err)
			}

			if got, err := interrupted.Status(); err != nil || got != execution.Queued || calls.Load() != 0 {
				t.Fatalf("unstarted request after restart = %q, %v; calls = %d", got, err, calls.Load())
			}
		})
	}
}

func TestQueuedCancellationRequiresPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newQueueFailingStore(execution.Cancelled)
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

	store.failing.Store(true)

	if err := opened.Cancel(ctx, queued.ID()); !errors.Is(err, store.err) {
		t.Fatalf("queued cancellation error = %v", err)
	}

	if status, err := queued.Status(); err != nil || status != execution.Queued {
		t.Fatalf("failed write changed queued work = %q, %v", status, err)
	}

	store.failing.Store(false)

	if err := opened.Cancel(ctx, queued.ID()); err != nil {
		t.Fatal(err)
	}

	if status, err := queued.Wait(ctx); err != nil || status != execution.Cancelled {
		t.Fatalf("cancelled execution = %q, %v", status, err)
	}

	close(executor.release)

	if status, err := running.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("running execution = %q, %v", status, err)
	}
}

func TestShutdownBeforeDurableStartPreservesQueuedRequest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lifetime, shutdown := context.WithCancel(ctx)
	defer shutdown()

	store := &blockingStartStore{Store: entry.NewMemoryStore(), started: make(chan struct{}), release: make(chan struct{})}

	var calls atomic.Int32

	executor := execution.Func(func(context.Context, execution.ExecuteParams) error {
		calls.Add(1)

		return nil
	})
	opened := newExecutorSession(lifetime, t, "session", store, "work", executor)
	accepted := make(chan *Execution, 1)

	admissionErrors := make(chan error, 1)
	go func() {
		current, err := opened.Execute(ctx, execution.NewRequest("work", "saved input"))
		accepted <- current

		admissionErrors <- err
	}()

	select {
	case <-store.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	shutdown()

	var current *Execution
	select {
	case current = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := <-admissionErrors; err != nil {
		t.Fatalf("durable admission was lost: %v", err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("failed start = %v", err)
	}

	entries, err := store.List(ctx, "session", 0)
	if err != nil || len(entries) != 1 || entries[0].Type != execution.EntryType || calls.Load() != 0 {
		t.Fatalf("unstarted request became terminal: entries = %+v, %v; calls = %d", entries, err, calls.Load())
	}

	close(store.release)
	restored := newExecutorSession(ctx, t, "session", store, "work", executor)

	interrupted, err := restored.Execution(ctx, current.ID())
	if err != nil {
		t.Fatal(err)
	}

	if status, err := interrupted.Status(); err != nil || status != execution.Queued || calls.Load() != 0 {
		t.Fatalf("restored request = %q, %v; calls = %d", status, err, calls.Load())
	}

	resumed, err := restored.Resume(ctx)
	if err != nil || resumed != interrupted {
		t.Fatalf("resuming queued work replaced its handle: %v, %v", resumed, err)
	}

	if status, err := resumed.Wait(ctx); err != nil || status != execution.Completed || calls.Load() != 1 {
		t.Fatalf("resumed queued work = %q, %v; calls = %d", status, err, calls.Load())
	}
}

func TestResumeRequiresDurableStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newQueueFailingStore(execution.Running)

	broker := event.NewBroker("session", store)
	if err := broker.Commit(ctx, "work", execution.EntryType, execution.Record{
		ID: "work", Executor: "work", Input: json.RawMessage(`null`), Status: execution.Interrupted,
	}); err != nil {
		t.Fatal(err)
	}

	executor := newBlockingExecutor()
	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	interrupted, err := opened.Execution(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}

	caller, stopWaiting := context.WithCancel(ctx)
	stopWaiting()

	if resumed, err := opened.Resume(caller); resumed != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resume = %v, %v", resumed, err)
	}

	store.failing.Store(true)

	if resumed, err := opened.Resume(ctx); resumed != nil || !errors.Is(err, store.err) {
		t.Fatalf("uncommitted resume = %v, %v", resumed, err)
	}

	if status, err := interrupted.Status(); err != nil || status != execution.Interrupted {
		t.Fatalf("failed resume changed the execution = %q, %v", status, err)
	}

	select {
	case <-interrupted.Done():
		t.Fatal("failed resume closed the restored execution handle")
	default:
	}

	select {
	case <-executor.started:
		t.Fatal("executor ran before its running record was durable")
	default:
	}

	store.failing.Store(false)

	resumed, err := opened.Resume(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if resumed != interrupted {
		t.Fatal("resume replaced the restored execution handle")
	}

	if duplicate, err := opened.Resume(ctx); duplicate != nil || !errors.Is(err, ErrExecutionNotResumable) {
		t.Fatalf("duplicate resume = %v, %v", duplicate, err)
	}

	close(executor.release)

	if status, err := resumed.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("resumed work = %q, %v", status, err)
	}

	if duplicate, err := opened.Resume(ctx); duplicate != nil || !errors.Is(err, ErrExecutionNotResumable) {
		t.Fatalf("completed resume = %v, %v", duplicate, err)
	}

	opened.Close()

	if resumed, err := opened.Resume(ctx); resumed != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed resume = %v, %v", resumed, err)
	}
}

func TestCancelInterruptedExecutionAdvancesQueueAfterCommit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newQueueFailingStore(execution.Cancelled)

	broker := event.NewBroker("session", store)
	for _, record := range []execution.Record{
		{ID: "first", Executor: "work", Status: execution.Interrupted},
		{ID: "second", Executor: "work", Status: execution.Queued},
	} {
		if err := broker.Commit(ctx, record.ID, execution.EntryType, record); err != nil {
			t.Fatal(err)
		}
	}

	started := make(chan string, 1)
	opened := newExecutorSession(ctx, t, "session", store, "work", execution.Func(
		func(_ context.Context, params execution.ExecuteParams) error {
			started <- params.ExecutionID

			return nil
		},
	))

	interrupted, err := opened.Execution(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}

	queued, err := opened.Execution(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}

	store.failing.Store(true)

	if err := opened.Cancel(ctx, "first"); !errors.Is(err, store.err) {
		t.Fatalf("uncommitted cancellation = %v", err)
	}

	select {
	case id := <-started:
		t.Fatalf("queue advanced before cancellation was durable: %s", id)
	default:
	}

	store.failing.Store(false)

	if err := opened.Cancel(ctx, "first"); err != nil {
		t.Fatal(err)
	}

	if status, err := interrupted.Wait(ctx); err != nil || status != execution.Cancelled {
		t.Fatalf("cancelled interrupted execution = %q, %v", status, err)
	}

	if id := receiveStarted(ctx, t, started); id != "second" {
		t.Fatalf("cancelled execution was resumed: %s", id)
	}

	if status, err := queued.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("next execution = %q, %v", status, err)
	}
}

type blockingStartStore struct {
	entry.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingStartStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range entries {
		if value.Type != execution.EntryType {
			continue
		}

		var saved execution.Record
		if err := json.Unmarshal(value.Payload, &saved); err != nil {
			return nil, err
		}

		if saved.Status != execution.Running {
			continue
		}

		s.once.Do(func() { close(s.started) })

		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}

type queueFailingStore struct {
	entry.Store
	status  execution.Status
	err     error
	failing atomic.Bool
}

func newQueueFailingStore(status execution.Status) *queueFailingStore {
	return &queueFailingStore{Store: entry.NewMemoryStore(), status: status, err: errors.New("storage unavailable")}
}

func (s *queueFailingStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range entries {
		if s.failing.Load() && value.Type == execution.EntryType {
			var saved execution.Record
			if err := json.Unmarshal(value.Payload, &saved); err != nil {
				return nil, err
			}

			if saved.Status == s.status {
				return nil, s.err
			}
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}

func receiveStarted(ctx context.Context, t *testing.T, started <-chan string) string {
	t.Helper()

	select {
	case input := <-started:
		return input
	case <-ctx.Done():
		t.Fatal(ctx.Err())

		return ""
	}
}
