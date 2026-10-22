package harness

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestSessionCreatesMetadataAndSharesInstance(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	h := newTestHarness(t, store, kittest.NewModel(t))

	created, err := h.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	state, err := store.sessions.Get(context.Background(), created.ID())
	if err != nil {
		t.Fatal(err)
	}

	if created.ID() == "" || state.ID != created.ID() || state.CreatedAt.IsZero() || state.UpdatedAt != state.CreatedAt {
		t.Fatalf("persisted metadata = %+v", state)
	}

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
}

func TestSessionCreatesDistinctSessionsForEmptyID(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, newTestStore(), kittest.NewModel(t))

	first, err := h.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	if first.ID() == second.ID() {
		t.Fatal("empty ID reused an existing session")
	}
}

func TestSessionReturnsNotFoundForMissingID(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	h := newTestHarness(t, store, kittest.NewModel(t))

	if _, err := h.Session(context.Background(), "missing"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Session() error = %v", err)
	}

	states, err := store.sessions.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(states) != 0 {
		t.Fatalf("missing lookup created metadata: %+v", states)
	}
}

func TestSessionLoadsPersistedMetadata(t *testing.T) {
	t.Parallel()

	store := newTestStore()

	state := session.State{ID: "existing", CreatedAt: time.Now().UTC().Add(-time.Hour)}
	if err := store.sessions.Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, store, kittest.NewModel(t))

	opened, err := h.Session(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}

	persisted, err := store.sessions.Get(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}

	if opened.ID() != state.ID || persisted != state {
		t.Fatalf("existing metadata changed: %+v", persisted)
	}
}

func TestConcurrentSessionLookupSharesInstance(t *testing.T) {
	t.Parallel()

	metadata := &countingSessionStore{Store: newMemorySessionStore()}
	if err := metadata.Save(context.Background(), session.State{ID: "shared"}); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, testStore{metadata, entry.NewMemoryStore()}, kittest.NewModel(t))

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

	if metadata.calls.Load() != 1 {
		t.Fatalf("session lookups = %d", metadata.calls.Load())
	}
}

func TestLookupContextDoesNotOwnExecution(t *testing.T) {
	t.Parallel()

	model := &lifetimeModel{started: make(chan struct{}), release: make(chan struct{})}
	h := newTestHarness(t, newTestStore(), model)

	ctx, cancel := context.WithCancel(context.Background())

	opened, err := h.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	id, err := opened.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("work"))})
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, model.started)
	cancel()

	if err := h.ctx.Err(); err != nil {
		t.Fatalf("lookup cancellation reached harness: %v", err)
	}

	close(model.release)

	finished := waitFinished(t, opened, id)
	if finished.Outcome != execution.Completed {
		t.Fatalf("execution outcome = %+v", finished)
	}
}

func TestShutdownOwnsSessionsAndRejectsFurtherOpens(t *testing.T) {
	t.Parallel()

	model := &lifetimeModel{started: make(chan struct{}), release: make(chan struct{})}
	h := newTestHarness(t, newTestStore(), model)

	opened, err := h.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := opened.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	id, err := opened.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("work"))})
	if err != nil {
		t.Fatal(err)
	}

	waitSignal(t, model.started)

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitSignal(t, subscription.Done())

	if finished := waitFinished(t, opened, id); finished.Outcome != execution.Cancelled {
		t.Fatalf("execution outcome = %+v", finished)
	}

	if _, err := h.Session(context.Background(), ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("new session after shutdown error = %v", err)
	}

	if _, err := h.Session(context.Background(), opened.ID()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Session after shutdown error = %v", err)
	}

	if _, err := opened.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.Message{}}); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("execution after shutdown error = %v", err)
	}

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated Shutdown() error = %v", err)
	}
}

func TestShutdownDeadlineDoesNotAbandonCleanup(t *testing.T) {
	t.Parallel()

	model := &lifetimeModel{
		started:      make(chan struct{}),
		release:      make(chan struct{}),
		ignoreCancel: true,
	}
	h := newTestHarness(t, newTestStore(), model)

	opened, err := h.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := opened.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("work"))}); err != nil {
		t.Fatal(err)
	}

	waitSignal(t, model.started)
	defer close(model.release)

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

	h := newTestHarness(t, newTestStore(), kittest.NewModel(t))
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

	h := newTestHarness(t, newTestStore(), kittest.NewModel(t))

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

	store, err := jsonl.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent("saved response")),
		FinishReason: kit.FinishReasonStop,
	}})
	first := newTestHarness(t, store, model)

	opened, err := first.Session(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	id, err := opened.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("saved input"))})
	if err != nil {
		t.Fatal(err)
	}

	waitFinished(t, opened, id)

	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := newTestHarness(t, store, kittest.NewModel(t))

	reopened, err := second.Session(context.Background(), opened.ID())
	if err != nil {
		t.Fatal(err)
	}

	if reopened == opened {
		t.Fatal("new host reused a closed session instance")
	}

	if finished := waitFinished(t, reopened, id); finished.Outcome != execution.Completed {
		t.Fatalf("restored execution = %+v", finished)
	}

	metadata, err := store.SessionStore().Get(context.Background(), opened.ID())
	if err != nil || metadata.ID != opened.ID() {
		t.Fatalf("restored metadata = %+v, error = %v", metadata, err)
	}
}

func waitFinished(t *testing.T, opened *session.Session, id string) execution.Finished {
	t.Helper()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for {
		entries, err := opened.Entries(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}

		for _, value := range entries {
			if value.ExecutionID == id && value.Type == execution.FinishedEntryType {
				var finished execution.Finished
				if err := json.Unmarshal(value.Payload, &finished); err != nil {
					t.Fatal(err)
				}

				return finished
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("execution did not finish")
		}
	}
}

func newTestHarness(t *testing.T, store Store, model kit.Model) *Harness {
	t.Helper()

	h := New(store, WithExecutors(map[string]execution.Executor{execution.AgentName: execution.NewAgent(agent.New(kit.AgentConfig{}, testModelResolver{model}))}))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.Shutdown(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	return h
}

type testModelResolver struct{ model kit.Model }

func (r testModelResolver) Resolve(context.Context) (kit.Model, error) { return r.model, nil }

type testStore struct {
	sessions session.Store
	entries  entry.Store
}

func newTestStore() testStore {
	return testStore{newMemorySessionStore(), entry.NewMemoryStore()}
}

func (s testStore) SessionStore() session.Store { return s.sessions }
func (s testStore) EntryStore() entry.Store     { return s.entries }

type memorySessionStore struct {
	mu     sync.Mutex
	states map[string]session.State
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{states: make(map[string]session.State)}
}

func (s *memorySessionStore) Get(ctx context.Context, id string) (session.State, error) {
	if err := ctx.Err(); err != nil {
		return session.State{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[id]
	if !ok {
		return session.State{}, session.ErrNotFound
	}

	return state, nil
}

func (s *memorySessionStore) Save(ctx context.Context, state session.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[state.ID] = state

	return nil
}

func (s *memorySessionStore) List(ctx context.Context) ([]session.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	states := make([]session.State, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}

	return states, nil
}

func (s *memorySessionStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, id)

	return nil
}

type countingSessionStore struct {
	session.Store
	calls atomic.Int32
}

func (s *countingSessionStore) Get(ctx context.Context, id string) (session.State, error) {
	s.calls.Add(1)

	return s.Store.Get(ctx, id)
}

type lifetimeModel struct {
	started      chan struct{}
	release      chan struct{}
	ignoreCancel bool
}

func (*lifetimeModel) Config() kit.ModelConfig { return kit.ModelConfig{ID: "lifetime"} }

func (m *lifetimeModel) Generate(ctx context.Context, _ kit.ModelRequest) (kit.ModelResponse, error) {
	close(m.started)

	if m.ignoreCancel {
		<-m.release
	} else {
		select {
		case <-m.release:
		case <-ctx.Done():
			return kit.ModelResponse{}, ctx.Err()
		}
	}

	return kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent("done")),
		FinishReason: kit.FinishReasonStop,
	}, nil
}

func (m *lifetimeModel) Stream(ctx context.Context, request kit.ModelRequest) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.StreamFromGenerate(ctx, request, m.Generate)
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

func TestRunLeavesSessionHistoryUntouched(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	if err := store.sessions.Save(context.Background(), session.State{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	bus := event.NewBus("persisted", store.entries)
	if err := bus.Commit(context.Background(), "unfinished", execution.StartedEntryType, execution.Started{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	h := New(store, WithExecutors(map[string]execution.Executor{
		"work": execution.Func(func(context.Context, execution.ExecuteParams) error { return nil }),
	}))

	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.Session(context.Background(), "persisted")
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() { result <- h.Run(context.Background()) }()

	waitRunning(t, h)

	entries, err := opened.Entries(context.Background(), 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("starting harness changed history: %+v, %v", entries, err)
	}

	if _, err := opened.Execute(context.Background(), execution.Request{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	if finished := waitFinished(t, opened, "unfinished"); finished.Outcome != execution.Interrupted {
		t.Fatalf("previous outcome = %+v", finished)
	}

	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := waitError(t, result); err != nil {
		t.Fatal(err)
	}
}
