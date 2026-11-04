package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestCreateSessionPersistsInitialValuesAndSharesInstance(t *testing.T) {
	t.Parallel()

	sessions := newObservedSessionStore()
	store := testStore{sessions, entry.NewMemoryStore()}
	h := newTestHarness(t, store)

	created, err := h.CreateSession(context.Background(), map[string]any{"workdir": "/workspace", "attempt": 1})
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.sessions.Get(context.Background(), created.ID())
	if err != nil {
		t.Fatal(err)
	}

	if created.ID() == "" || record.ID != created.ID() || record.CreatedAt.IsZero() || record.UpdatedAt != record.CreatedAt {
		t.Fatalf("persisted state = %+v", record)
	}

	if string(record.Values["workdir"]) != `"/workspace"` || string(record.Values["attempt"]) != "1" {
		t.Fatalf("initial values = %v", record.Values)
	}

	if sessions.saves.Load() != 1 || sessions.updates.Load() != 0 {
		t.Fatal("creation did not persist initial state in one save")
	}

	sessions.getErr = errors.New("session storage unavailable")

	const clients = 20

	results := make(chan *session.Session, clients)
	errors := make(chan error, clients)

	var callers sync.WaitGroup
	for range clients {
		callers.Go(func() {
			opened, err := h.Session(context.Background(), created.ID())
			results <- opened

			errors <- err
		})
	}

	callers.Wait()

	for range clients {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}

		if got := <-results; got != created {
			t.Fatal("clients received different instances of one session")
		}
	}

	if sessions.gets.Load() != 1 {
		t.Fatal("cached session lookup reread storage")
	}
}

func TestCreateSessionCreatesDistinctSessions(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore())

	first, err := h.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID() == second.ID() {
		t.Fatal("creation reused an existing session")
	}
}

func TestCreateSessionRejectsInvalidInitialValuesBeforeSaving(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	h := newTestHarness(t, store)

	opened, err := h.CreateSession(context.Background(), map[string]any{"invalid": func() {}})
	if opened != nil || err == nil {
		t.Fatalf("invalid initial values = %v, %v", opened, err)
	}

	records, err := store.sessions.List(context.Background())
	if err != nil || len(records) != 0 {
		t.Fatalf("invalid creation persisted a session: %v, %v", records, err)
	}
}

func TestSessionOperationsRequireID(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore())
	if _, err := h.Session(context.Background(), ""); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("empty lookup = %v", err)
	}

	if err := h.DeleteSession(context.Background(), ""); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("empty deletion = %v", err)
	}
}

func TestDeleteSessionStopsWorkBeforeRemovingPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	accessed := make(chan error, 1)

	var h *Harness

	h = newTestHarness(t, storage, WithExecutor("work", execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		close(started)
		<-ctx.Done()

		_, err := h.Session(context.Background(), params.SessionID)
		accessed <- err

		return ctx.Err()
	})))

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	path, err := storage.PutBlob(ctx, opened.ID(), []byte("saved output"))
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, started)

	deleted := make(chan error, 1)
	go func() { deleted <- h.DeleteSession(ctx, opened.ID()) }()

	if err := waitError(t, deleted); err != nil {
		t.Fatal(err)
	}

	if err := waitError(t, accessed); err != nil {
		t.Fatalf("deletion blocked execution cleanup from accessing the harness: %v", err)
	}

	waitSignal(t, subscription.Done())

	if status, err := current.Wait(ctx); err != nil || status != execution.Interrupted {
		t.Fatalf("deleted execution = %q, %v", status, err)
	}

	if _, err := h.Session(ctx, opened.ID()); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("deleted session lookup = %v", err)
	}

	if _, err := opened.Execute(ctx, execution.NewRequest("work", nil)); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("deleted runtime admitted work: %v", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted session retained its blob: %v", err)
	}

	if err := h.DeleteSession(ctx, opened.ID()); err != nil {
		t.Fatalf("repeated deletion = %v", err)
	}
}

func TestDeleteSessionDoesNotConstructUnopenedRuntime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	entries := newFailingEntryStore(errors.New("journal unavailable"))
	entries.readFailing.Store(true)

	store := testStore{state.NewMemoryStore(), entries}
	if err := store.sessions.Save(ctx, state.Record{ID: "unopened"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, store)
	if err := h.DeleteSession(ctx, "unopened"); err != nil {
		t.Fatalf("deleting unopened session read its journal: %v", err)
	}

	if entries.reads.Load() != 0 {
		t.Fatal("deletion initialized execution recovery")
	}

	if _, err := store.sessions.Get(ctx, "unopened"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("session record survived deletion: %v", err)
	}
}

func TestDeleteSessionFailureKeepsClosedRuntimeUntilRetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newObservedSessionStore()
	sessions.deleteErr = errors.New("delete unavailable")
	h := newTestHarness(t, testStore{sessions, entry.NewMemoryStore()})

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.DeleteSession(ctx, opened.ID()); !errors.Is(err, sessions.deleteErr) {
		t.Fatalf("failed deletion = %v", err)
	}

	attached, err := h.Session(ctx, opened.ID())
	if err != nil || attached != opened {
		t.Fatalf("failed deletion replaced the stopped runtime: %v, %v", attached, err)
	}

	if _, err := attached.Execute(ctx, execution.NewRequest("work", nil)); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("failed deletion restarted work: %v", err)
	}

	sessions.deleteErr = nil

	if err := h.DeleteSession(ctx, opened.ID()); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Session(ctx, opened.ID()); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("successful retry retained runtime: %v", err)
	}
}

func TestSessionReturnsNotFoundForMissingID(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	h := newTestHarness(t, store)

	if _, err := h.Session(context.Background(), "missing"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Session() error = %v", err)
	}

	states, err := store.sessions.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(states) != 0 {
		t.Fatalf("missing lookup created state: %+v", states)
	}
}

func TestSessionLoadsPersistedState(t *testing.T) {
	t.Parallel()

	store := newTestStore()

	record := state.Record{ID: "existing", CreatedAt: time.Now().UTC().Add(-time.Hour)}
	if err := store.sessions.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, store)

	opened, err := h.Session(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}

	persisted, err := store.sessions.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if opened.ID() != record.ID || !reflect.DeepEqual(persisted, record) {
		t.Fatalf("existing state changed: %+v", persisted)
	}
}

func TestConcurrentSessionLookupInitializesOnce(t *testing.T) {
	t.Parallel()

	sessions := state.NewMemoryStore()
	if err := sessions.Save(context.Background(), state.Record{ID: "shared"}); err != nil {
		t.Fatal(err)
	}

	entries := newFailingEntryStore(nil)

	bus := event.NewBus("shared", entries)
	if err := bus.Commit(context.Background(), "unfinished", execution.EntryType, execution.Record{ID: "unfinished", Status: execution.Running}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})

	const clients = 20

	results := make(chan *session.Session, clients)
	errors := make(chan error, clients)

	var callers sync.WaitGroup
	for range clients {
		callers.Go(func() {
			opened, err := h.Session(context.Background(), "shared")
			results <- opened

			errors <- err
		})
	}

	callers.Wait()

	var shared *session.Session
	for range clients {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}

		opened := <-results
		if shared == nil {
			shared = opened
		} else if opened != shared {
			t.Fatal("clients received different instances of one session")
		}
	}

	stored, err := entries.List(context.Background(), "shared", 0)
	if err != nil || len(stored) != 2 || stored[1].Type != execution.EntryType || stored[1].ExecutionID != "unfinished" {
		t.Fatalf("reconciled entries = %+v, error = %v", stored, err)
	}

	if reads := entries.reads.Load(); reads != 2 {
		t.Fatalf("concurrent opening read execution state %d times, want 2", reads)
	}
}

func TestLookupContextDoesNotOwnExecution(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	h := newTestHarness(t, newTestStore(), WithExecutor("work", executor))

	ctx, cancel := context.WithCancel(context.Background())

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, executor.started)
	cancel()

	if err := h.ctx.Err(); err != nil {
		t.Fatalf("lookup cancellation reached harness: %v", err)
	}

	close(executor.release)

	finished := waitFinished(t, opened, current.ID())
	if finished.Status != execution.Completed {
		t.Fatalf("execution status = %+v", finished)
	}
}

func TestShutdownOwnsSessionsAndRejectsFurtherOpens(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	h := newTestHarness(t, newTestStore(), WithExecutor("work", executor))

	opened, err := h.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := opened.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, executor.started)

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitSignal(t, subscription.Done())

	if finished := waitFinished(t, opened, current.ID()); finished.Status != execution.Interrupted {
		t.Fatalf("execution status = %+v", finished)
	}

	if _, err := h.CreateSession(context.Background(), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("new session after shutdown error = %v", err)
	}

	if _, err := h.Session(context.Background(), opened.ID()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Session after shutdown error = %v", err)
	}

	if _, err := opened.Execute(context.Background(), execution.NewRequest("work", nil)); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("execution after shutdown error = %v", err)
	}

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated Shutdown() error = %v", err)
	}
}

func TestShutdownDeadlineDoesNotAbandonCleanup(t *testing.T) {
	t.Parallel()

	executor := newBlockingExecutor()
	executor.ignoreCancel = true
	h := newTestHarness(t, newTestStore(), WithExecutor("work", executor))

	opened, err := h.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := opened.Execute(context.Background(), execution.NewRequest("work", nil)); err != nil {
		t.Fatal(err)
	}

	waitSignal(t, executor.started)
	defer close(executor.release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown deadline error = %v", err)
	}

	select {
	case <-h.done:
		t.Fatal("shutdown completed while invocation still owned resources")
	default:
	}
}

func TestRunCancellationShutsDownHost(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore())
	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan error, 1)
	go func() { result <- h.Run(ctx) }()

	waitRunning(t, h)

	if err := h.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run error = %v", err)
	}

	cancel()

	if err := waitError(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run cancellation error = %v", err)
	}

	waitSignal(t, h.done)

	if err := h.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after shutdown error = %v", err)
	}
}

func TestShutdownReleasesRun(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore())

	result := make(chan error, 1)
	go func() { result <- h.Run(context.Background()) }()

	waitRunning(t, h)

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := waitError(t, result); err != nil {
		t.Fatalf("Run during explicit shutdown error = %v", err)
	}
}

func TestSessionsRemainPersistentAcrossHosts(t *testing.T) {
	t.Parallel()

	path := t.TempDir()

	store, err := jsonl.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	first := newTestHarness(t, store, WithExecutor("work", execution.Func(func(context.Context, execution.ExecuteParams) error {
		return errors.New("saved failure")
	})))

	opened, err := first.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execute(context.Background(), execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	original := waitFinished(t, opened, current.ID())
	if original.Status != execution.Failed || original.Error != "saved failure" {
		t.Fatalf("original execution = %+v", original)
	}

	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := jsonl.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	second := newTestHarness(t, reopenedStore)

	reopened, err := second.Session(context.Background(), opened.ID())
	if err != nil {
		t.Fatal(err)
	}

	if reopened == opened {
		t.Fatal("new host reused a closed session instance")
	}

	attached, err := reopened.Execution(context.Background(), current.ID())
	if err != nil {
		t.Fatal(err)
	}

	restored, err := attached.Wait(context.Background())
	if err != nil || restored != original.Status {
		t.Fatalf("restored execution = %+v, %v", restored, err)
	}

	if finished := waitFinished(t, reopened, current.ID()); !reflect.DeepEqual(finished, original) {
		t.Fatalf("restored finish = %+v; want %+v", finished, original)
	}

	sessions, err := store.SessionStore().Get(context.Background(), opened.ID())
	if err != nil || sessions.ID != opened.ID() {
		t.Fatalf("restored sessions = %+v, error = %v", sessions, err)
	}
}

func TestSessionStatePersistsAndReachesExecutors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	h := New(storage, WithExecutor("inspect", execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		var workdir string
		if _, err := params.State.Get(ctx, "workdir", &workdir); err != nil {
			return err
		}

		if err := params.State.Set(ctx, "executor.workdir", workdir); err != nil {
			return err
		}

		return params.State.Set(ctx, "executor.session", params.SessionID)
	})))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	workdirs := []string{"/first", "/second"}

	ids := make([]string, 0, len(workdirs))
	for _, workdir := range workdirs {
		opened, err := h.CreateSession(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		ids = append(ids, opened.ID())
		if err := opened.State().Set(ctx, "workdir", workdir); err != nil {
			t.Fatal(err)
		}

		current, err := opened.Execute(ctx, execution.NewRequest("inspect", nil))
		if err != nil {
			t.Fatal(err)
		}

		if result, err := current.Wait(ctx); err != nil || result != execution.Completed {
			t.Fatalf("execution result = %+v, error = %v", result, err)
		}
	}

	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	reopenedStorage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	reopenedHarness := New(reopenedStorage)
	t.Cleanup(func() { _ = reopenedHarness.Shutdown(context.Background()) })

	for index, id := range ids {
		opened, err := reopenedHarness.Session(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		for key, want := range map[string]string{
			"workdir":          workdirs[index],
			"executor.workdir": workdirs[index],
			"executor.session": id,
		} {
			var value string
			if found, err := opened.State().Get(ctx, key, &value); err != nil || !found || value != want {
				t.Fatalf("session %s key %s = %q, found = %v, error = %v", id, key, value, found, err)
			}
		}
	}
}

func TestSessionInitializesBeforeReturning(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newTestStore()
	if err := store.sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	bus := event.NewBus("persisted", store.entries)
	if err := bus.Commit(context.Background(), "unfinished", execution.EntryType, execution.Record{ID: "unfinished", Status: execution.Running}); err != nil {
		t.Fatal(err)
	}

	h := New(store, WithExecutor("work", execution.Func(func(context.Context, execution.ExecuteParams) error { return nil })))

	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.Session(ctx, "persisted")
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	for _, want := range []string{execution.EntryType, execution.EntryType} {
		select {
		case value := <-subscription.Events():
			if value.Type != want || value.ExecutionID != "unfinished" {
				t.Fatalf("replayed event = %+v, want %s", value, want)
			}
		case <-ctx.Done():
			t.Fatal("reconciled history was not replayed")
		}
	}

	if finished := waitFinished(t, opened, "unfinished"); finished.Status != execution.Interrupted {
		t.Fatalf("previous status = %+v", finished)
	}

	current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("new execution status = %q, error = %v", status, err)
	}
}

func TestSessionAttachmentPreservesActiveExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executor := newBlockingExecutor()
	finish := sync.OnceFunc(func() { close(executor.release) })
	h := newTestHarness(t, newTestStore(), WithExecutor("work", executor))
	t.Cleanup(finish)

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, executor.started)

	for range 3 {
		attached, err := h.Session(ctx, opened.ID())
		if err != nil || attached != opened {
			t.Fatalf("active session attachment = %v, error = %v", attached, err)
		}
	}

	select {
	case <-current.Done():
		t.Fatal("attachment interrupted active work")
	default:
	}

	finish()

	if result, err := current.Wait(ctx); err != nil || result != execution.Completed {
		t.Fatalf("active execution = %+v, error = %v", result, err)
	}
}

func TestSessionOpeningRetriesFailedInitialization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	failure := errors.New("storage unavailable")
	entries := newFailingEntryStore(failure)
	entries.failing.Store(true)

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	bus := event.NewBus("persisted", entries)
	if err := bus.Commit(ctx, "unfinished", execution.EntryType, execution.Record{ID: "unfinished", Status: execution.Running}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})
	for range 2 {
		opened, err := h.Session(ctx, "persisted")
		if opened != nil || !errors.Is(err, failure) {
			t.Fatalf("failed reconciliation = %v, error = %v", opened, err)
		}
	}

	entries.failing.Store(false)

	opened, err := h.Session(ctx, "persisted")
	if err != nil {
		t.Fatal(err)
	}

	finished := waitFinished(t, opened, "unfinished")
	if finished.Status != execution.Interrupted {
		t.Fatalf("reconciled status = %+v", finished)
	}

	stored, err := opened.Entries(ctx, 0)
	if err != nil || len(stored) != 2 {
		t.Fatalf("reconciliation duplicated history: %+v, error = %v", stored, err)
	}
}

func TestSessionAttachmentDoesNotRetryFailedSettlement(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	failure := errors.New("storage unavailable")
	entries := newFailingEntryStore(failure)

	var calls atomic.Int32

	h := New(testStore{state.NewMemoryStore(), entries}, WithExecutor("work", execution.Func(func(context.Context, execution.ExecuteParams) error {
		calls.Add(1)

		return errors.New("work failed")
	})))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries.failing.Store(true)

	current, err := opened.Execute(ctx, execution.NewRequest("work", nil))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, failure) {
		t.Fatalf("terminal persistence error = %v", err)
	}

	for _, failing := range []bool{true, false} {
		entries.failing.Store(failing)

		attached, err := h.Session(ctx, opened.ID())
		if attached != opened || err != nil {
			t.Fatalf("failed session attachment = %v, error = %v", attached, err)
		}

		if next, err := opened.Execute(ctx, execution.NewRequest("work", nil)); next != nil || !errors.Is(err, session.ErrClosed) {
			t.Fatalf("failed session admission = %v, error = %v", next, err)
		}

		if _, err := current.Wait(ctx); !errors.Is(err, failure) {
			t.Fatalf("execution lost persistence failure: %v", err)
		}
	}

	if calls.Load() != 1 {
		t.Fatal("failed settlement reran work or admitted another execution")
	}
}

func TestSessionAttachmentDoesNotReloadExecutionState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	entries := newFailingEntryStore(errors.New("journal unavailable"))

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})

	opened, err := h.Session(ctx, "persisted")
	if err != nil {
		t.Fatal(err)
	}

	reads := entries.reads.Load()

	entries.readFailing.Store(true)

	for range 3 {
		attached, err := h.Session(ctx, opened.ID())
		if err != nil || attached != opened {
			t.Fatalf("cached session attachment = %v, error = %v", attached, err)
		}
	}

	if entries.reads.Load() != reads {
		t.Fatal("cached session attachment reloaded execution state")
	}
}

func TestSessionAttachmentReturnsExistingClosedSession(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore())

	opened, err := h.CreateSession(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	opened.Close()

	attached, err := h.Session(context.Background(), opened.ID())
	if attached != opened || err != nil {
		t.Fatalf("closed session attachment = %v, error = %v", attached, err)
	}

	if _, err := attached.Execute(context.Background(), execution.NewRequest("work", nil)); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed session admission = %v", err)
	}
}

func TestSessionOpeningReturnsInitializationReadFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	failure := errors.New("journal unavailable")
	entries := newFailingEntryStore(failure)
	entries.readFailing.Store(true)

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})

	opened, err := h.Session(ctx, "persisted")
	if opened != nil || !errors.Is(err, failure) {
		t.Fatalf("journal read failure = %v, error = %v", opened, err)
	}

	entries.readFailing.Store(false)

	opened, err = h.Session(ctx, "persisted")
	if err != nil || opened.ID() != "persisted" {
		t.Fatalf("attachment after journal recovery = %v, error = %v", opened, err)
	}
}

func TestShutdownCancelsSessionInitialization(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries := newBlockingInitializationStore("blocked")

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "blocked"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})
	t.Cleanup(sync.OnceFunc(func() { close(entries.release) }))

	result := make(chan error, 1)
	go func() {
		_, err := h.Session(context.Background(), "blocked")
		result <- err
	}()

	waitSignal(t, entries.started)

	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown while reconciling = %v", err)
	}

	if err := waitError(t, result); !errors.Is(err, ErrClosed) {
		t.Fatalf("attachment after shutdown = %v", err)
	}
}

func TestSessionOpeningWaitsForInitialization(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries := newBlockingInitializationStore("persisted")

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})
	release := sync.OnceFunc(func() { close(entries.release) })
	t.Cleanup(release)

	results := make(chan *session.Session, 2)

	errors := make(chan error, 2)
	for range 2 {
		go func() {
			opened, err := h.Session(ctx, "persisted")
			results <- opened

			errors <- err
		}()
	}

	waitSignal(t, entries.started)

	select {
	case <-results:
		t.Fatal("session was exposed before initialization completed")
	default:
	}

	release()

	var shared *session.Session
	for range 2 {
		if err := waitError(t, errors); err != nil {
			t.Fatal(err)
		}

		opened := <-results
		if shared != nil && shared != opened {
			t.Fatal("concurrent openers received different sessions")
		}

		shared = opened
	}
}

func TestSessionConstructionSurvivesCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries := newBlockingInitializationStore("persisted")

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})
	release := sync.OnceFunc(func() { close(entries.release) })
	t.Cleanup(release)

	opening, cancelOpening := context.WithCancel(ctx)
	defer cancelOpening()

	result := make(chan error, 1)
	go func() {
		_, err := h.Session(opening, "persisted")
		result <- err
	}()

	waitSignal(t, entries.started)
	cancelOpening()
	release()

	if err := waitError(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled opening = %v", err)
	}

	opened, err := h.Session(ctx, "persisted")
	if err != nil || opened.ID() != "persisted" {
		t.Fatalf("opening after cancellation = %v, error = %v", opened, err)
	}

	if reads := entries.reads.Load(); reads != 1 {
		t.Fatalf("caller cancellation restarted construction: journal reads = %d", reads)
	}
}

func TestWaitingClientCancellationPreservesSessionConstruction(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries := newBlockingInitializationStore("persisted")

	sessions := state.NewMemoryStore()
	if err := sessions.Save(ctx, state.Record{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{sessions, entries})
	release := sync.OnceFunc(func() { close(entries.release) })
	t.Cleanup(release)

	result := make(chan error, 1)
	go func() {
		_, err := h.Session(ctx, "persisted")
		result <- err
	}()

	waitSignal(t, entries.started)

	waiting, stopWaiting := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stopWaiting()

	waiter := make(chan error, 1)
	go func() {
		_, err := h.Session(waiting, "persisted")
		waiter <- err
	}()

	<-waiting.Done()

	select {
	case err := <-result:
		t.Fatalf("waiting client ended construction: %v", err)
	default:
	}

	release()

	if err := waitError(t, waiter); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled waiter = %v", err)
	}

	if err := waitError(t, result); err != nil {
		t.Fatalf("construction after waiter cancelled = %v", err)
	}
}

func newTestHarness(t *testing.T, store Store, options ...Option) *Harness {
	t.Helper()

	h := New(store, options...)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.Shutdown(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	return h
}

type testStore struct {
	sessions state.Store
	entries  entry.Store
}

func newTestStore() testStore {
	return testStore{state.NewMemoryStore(), entry.NewMemoryStore()}
}

func (s testStore) SessionStore() state.Store { return s.sessions }

func (s testStore) EntryStore() entry.Store { return s.entries }

type observedSessionStore struct {
	state.Store
	gets      atomic.Int32
	saves     atomic.Int32
	updates   atomic.Int32
	getErr    error
	deleteErr error
}

func newObservedSessionStore() *observedSessionStore {
	return &observedSessionStore{Store: state.NewMemoryStore()}
}

func (s *observedSessionStore) Get(ctx context.Context, id string) (state.Record, error) {
	s.gets.Add(1)

	if s.getErr != nil {
		return state.Record{}, s.getErr
	}

	return s.Store.Get(ctx, id)
}

func (s *observedSessionStore) Save(ctx context.Context, record state.Record) error {
	s.saves.Add(1)

	return s.Store.Save(ctx, record)
}

func (s *observedSessionStore) Update(ctx context.Context, id string, update func(*state.Record) error) error {
	s.updates.Add(1)

	return s.Store.Update(ctx, id, update)
}

func (s *observedSessionStore) Delete(ctx context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

	return s.Store.Delete(ctx, id)
}

type blockingExecutor struct {
	started      chan struct{}
	release      chan struct{}
	ignoreCancel bool
}

func newBlockingExecutor() *blockingExecutor {
	return &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
}

func (e *blockingExecutor) Execute(ctx context.Context, _ execution.ExecuteParams) error {
	close(e.started)

	if e.ignoreCancel {
		<-e.release

		return nil
	}

	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func waitError(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for result")

		return nil
	}
}

func waitRunning(t *testing.T, h *Harness) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		h.mu.Lock()
		running := h.running
		h.mu.Unlock()

		if running {
			return
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Run did not start")
		}
	}
}

type failingEntryStore struct {
	entry.Store
	failure     error
	failing     atomic.Bool
	readFailing atomic.Bool
	reads       atomic.Int32
}

func newFailingEntryStore(failure error) *failingEntryStore {
	return &failingEntryStore{Store: entry.NewMemoryStore(), failure: failure}
}

func (s *failingEntryStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range entries {
		if value.Type == execution.EntryType && s.failing.Load() {
			var saved execution.Record
			if err := json.Unmarshal(value.Payload, &saved); err != nil {
				return nil, err
			}

			if saved.Status.Terminal() {
				return nil, s.failure
			}
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}

func (s *failingEntryStore) List(ctx context.Context, sessionID string, afterID uint64) ([]entry.Entry, error) {
	s.reads.Add(1)

	if s.readFailing.Load() {
		return nil, s.failure
	}

	return s.Store.List(ctx, sessionID, afterID)
}

type blockingInitializationStore struct {
	entry.Store
	sessionID string
	started   chan struct{}
	release   chan struct{}
	start     sync.Once
	reads     atomic.Int32
}

func newBlockingInitializationStore(sessionID string) *blockingInitializationStore {
	return &blockingInitializationStore{
		Store:     entry.NewMemoryStore(),
		sessionID: sessionID,
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (s *blockingInitializationStore) List(ctx context.Context, sessionID string, afterID uint64) ([]entry.Entry, error) {
	if sessionID == s.sessionID {
		s.reads.Add(1)
		s.start.Do(func() { close(s.started) })

		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return s.Store.List(ctx, sessionID, afterID)
}

func waitFinished(t *testing.T, opened *session.Session, id string) execution.Record {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	current, err := opened.Execution(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ExecutionID != id || entries[i].Type != execution.EntryType {
			continue
		}

		var saved execution.Record
		if err := json.Unmarshal(entries[i].Payload, &saved); err != nil {
			t.Fatal(err)
		}

		return saved
	}

	t.Fatalf("execution %s has no persisted record", id)

	return execution.Record{}
}
