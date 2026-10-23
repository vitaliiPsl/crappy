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

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestExecuteMaintainsContext(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("first response")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 10, OutputTokens: 2},
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("second response")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 20, OutputTokens: 4},
		}},
	)
	store := entry.NewMemoryStore()

	session := newAgentSession(context.Background(),
		"session-1",
		store,
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	firstInput := kit.NewUserMessage(kit.NewTextContent("first input"))

	firstExecution, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: firstInput})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	secondExecution, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("second input"))})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	request := model.CallAt(1)
	if got, want := len(request.Messages), 3; got != want {
		t.Fatalf("second model request has %d messages, want %d", got, want)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if got, want := len(entries), 10; got != want {
		t.Fatalf("Entries() returned %d entries, want %d", got, want)
	}

	wantTypes := []string{
		execution.StartedEntryType,
		"message",
		"message",
		agent.FinishedEntryType,
		execution.FinishedEntryType,
		execution.StartedEntryType,
		"message",
		"message",
		agent.FinishedEntryType,
		execution.FinishedEntryType,
	}
	for i, wantType := range wantTypes {
		if entries[i].Type != wantType {
			t.Fatalf("entry %d type = %q, want %q", i, entries[i].Type, wantType)
		}
	}

	for i, value := range entries[:5] {
		if value.ExecutionID != firstExecution.ID() {
			t.Fatalf("first execution entry %d ID = %q, want %q", i, value.ExecutionID, firstExecution.ID())
		}
	}

	for i, value := range entries[5:] {
		if value.ExecutionID != secondExecution.ID() {
			t.Fatalf("second execution entry %d ID = %q, want %q", i, value.ExecutionID, secondExecution.ID())
		}
	}

	var firstStarted execution.Started
	if err := json.Unmarshal(entries[0].Payload, &firstStarted); err != nil {
		t.Fatalf("decode first started execution: %v", err)
	}

	var firstMessage kit.Message
	if err := json.Unmarshal(firstStarted.Input, &firstMessage); err != nil {
		t.Fatal(err)
	}

	if got, want := firstMessage.Content[0].Text.Text, "first input"; got != want {
		t.Fatalf("first execution input = %q, want %q", got, want)
	}

	var firstOutput kit.Message
	if err := json.Unmarshal(entries[2].Payload, &firstOutput); err != nil {
		t.Fatalf("decode first output: %v", err)
	}

	if got, want := firstOutput.Content[0].Text.Text, "first response"; got != want {
		t.Fatalf("first output = %q, want %q", got, want)
	}

	var firstFinished execution.Result
	if err := json.Unmarshal(entries[4].Payload, &firstFinished); err != nil {
		t.Fatalf("decode first finished execution: %v", err)
	}

	if got, want := firstFinished.Outcome, execution.Completed; got != want {
		t.Fatalf("first execution outcome = %q, want %q", got, want)
	}

	var firstResult agent.Finished
	if err := json.Unmarshal(entries[3].Payload, &firstResult); err != nil {
		t.Fatal(err)
	}

	if got, want := firstResult.Usage.InputTokens, int64(10); got != want {
		t.Fatalf("first result input tokens = %d, want %d", got, want)
	}

	if got, want := firstResult.LastUsage.OutputTokens, int64(2); got != want {
		t.Fatalf("first result last output tokens = %d, want %d", got, want)
	}

	var secondOutput kit.Message
	if err := json.Unmarshal(entries[7].Payload, &secondOutput); err != nil {
		t.Fatalf("decode second output: %v", err)
	}

	if got, want := secondOutput.Content[0].Text.Text, "second response"; got != want {
		t.Fatalf("second output = %q, want %q", got, want)
	}

	var secondFinished agent.Finished
	if err := json.Unmarshal(entries[8].Payload, &secondFinished); err != nil {
		t.Fatalf("decode second finished execution: %v", err)
	}

	if got, want := secondFinished.Usage.OutputTokens, int64(4); got != want {
		t.Fatalf("second result output tokens = %d, want %d", got, want)
	}
}

func TestExecuteRejectsConcurrentExecution(t *testing.T) {
	t.Parallel()

	model := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	session := newAgentSession(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("first input"))})
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

	<-model.started

	_, err = session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("second input"))})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Execute() error = %v, want ErrBusy", err)
	}

	if err := session.Recover(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("recovery during execution = %v, want ErrBusy", err)
	}

	close(model.release)
	waitSessionIdle(t, session)
}

func TestExecuteRecordsFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("model failed")
	model := kittest.NewModel(t, kittest.ModelResult{Error: wantErr})

	session := newAgentSession(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	last := entries[len(entries)-1]
	if last.ExecutionID != current.ID() || last.Type != execution.FinishedEntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, current.ID())
	}

	var finished execution.Result
	if err := json.Unmarshal(last.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, execution.Failed; got != want {
		t.Fatalf("execution outcome = %q, want %q", got, want)
	}

	if got, want := finished.Error, wantErr.Error(); got != want {
		t.Fatalf("execution error = %q, want %q", got, want)
	}
}

func TestCancelActiveExecution(t *testing.T) {
	t.Parallel()

	model := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	session := newAgentSession(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started

	if err := session.Cancel(context.Background(), "other-execution"); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(other) error = %v, want ErrExecutionNotActive", err)
	}

	if err := session.Cancel(context.Background(), current.ID()); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Result
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, execution.Cancelled; got != want {
		t.Fatalf("execution outcome = %q, want %q", got, want)
	}

	if err := session.Cancel(context.Background(), current.ID()); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(completed) error = %v, want ErrExecutionNotActive", err)
	}
}

func TestSessionLifetimeCancelsActiveExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	model := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	session := newAgentSession(ctx,
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	if _, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started
	cancel()
	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Result
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Outcome != execution.Cancelled {
		t.Fatalf("execution outcome = %q, want %q", finished.Outcome, execution.Cancelled)
	}
}

func TestCloseRejectsExecution(t *testing.T) {
	t.Parallel()

	session := newAgentSession(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}))

	session.Close()
	session.Close()

	_, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Execute() error = %v, want ErrClosed", err)
	}
}

func TestCloseCancelsActiveExecution(t *testing.T) {
	t.Parallel()

	model := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	session := newAgentSession(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started
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

	var finished execution.Result
	if err := json.Unmarshal(last.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Outcome != execution.Cancelled {
		t.Fatalf("execution outcome = %q, want %q", finished.Outcome, execution.Cancelled)
	}
}

func waitSessionIdle(t *testing.T, session *Session) {
	t.Helper()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()

	for {
		session.mu.Lock()
		idle := session.current == nil
		session.mu.Unlock()

		if idle {
			return
		}

		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("timed out waiting for session to become idle")
		}
	}
}

type blockingModel struct {
	started chan struct{}
	release chan struct{}
}

type finishFailingStore struct {
	entry.Store
	err      error
	failing  atomic.Bool
	finishes atomic.Int32
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

func (m *blockingModel) Config() kit.ModelConfig {
	return kit.ModelConfig{ID: "blocking"}
}

func (m *blockingModel) Generate(
	ctx context.Context,
	_ kit.ModelRequest,
) (kit.ModelResponse, error) {
	return m.response(ctx)
}

func (m *blockingModel) Stream(
	ctx context.Context,
	_ kit.ModelRequest,
) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.NewStream(func(kit.Emitter[kit.ModelEvent]) (kit.ModelResponse, error) {
		return m.response(ctx)
	})
}

func (m *blockingModel) response(ctx context.Context) (kit.ModelResponse, error) {
	close(m.started)

	select {
	case <-m.release:
		return kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}, nil
	case <-ctx.Done():
		return kit.ModelResponse{}, ctx.Err()
	}
}

func TestFunctionExecutionUsesSessionLifetimeAndDetachedInput(t *testing.T) {
	t.Parallel()

	started := make(chan execution.ExecuteParams, 1)
	release := make(chan struct{})
	store := entry.NewMemoryStore()

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) (any, error) {
		started <- params

		<-release

		return nil, params.Events.Commit(ctx, params.ExecutionID, "function.output", params.Input)
	})

	session := newExecutorSession(context.Background(), "session-1", store, "function", executor)

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
	waitSessionIdle(t, session)

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

	var finished execution.Result
	if err := json.Unmarshal(entries[2].Payload, &finished); err != nil {
		t.Fatal(err)
	}

	if finished.Outcome != execution.Completed {
		t.Fatalf("client cancellation affected invocation: %+v", finished)
	}
}

func TestExecuteValidatesBeforeRecording(t *testing.T) {
	t.Parallel()

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		t.Error("invalid request reached executor")

		return nil, nil
	})

	session := newExecutorSession(context.Background(), "session-1", entry.NewMemoryStore(), "work", executor)

	for _, request := range []execution.Request{
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

func TestTerminalPersistenceFailureRetainsOutcome(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{nil, errors.New("function failed")} {
		t.Run(fmt.Sprint(runErr), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}
			store.failing.Store(true)

			var calls atomic.Int32

			executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
				calls.Add(1)

				return nil, runErr
			})

			session := newExecutorSession(context.Background(), "session-1", store, "work", executor)

			request := execution.Request{Executor: "work"}

			first, err := session.Execute(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := first.Wait(ctx); !errors.Is(err, store.err) {
				t.Fatalf("terminal persistence error = %v", err)
			}

			if err := session.Cancel(context.Background(), first.ID()); !errors.Is(err, ErrExecutionNotActive) {
				t.Fatalf("cancel after invocation returned = %v", err)
			}

			if _, err := session.Execute(context.Background(), request); !errors.Is(err, ErrBusy) {
				t.Fatalf("submission after failed commit = %v", err)
			}

			if err := session.Recover(ctx); !errors.Is(err, store.err) {
				t.Fatalf("retry after failed commit = %v", err)
			}

			store.failing.Store(false)

			if _, err := session.Execute(ctx, request); !errors.Is(err, ErrBusy) {
				t.Fatalf("submission retried persistence = %v", err)
			}

			if err := session.Recover(ctx); err != nil {
				t.Fatal(err)
			}

			if _, err := session.Execute(context.Background(), request); err != nil {
				t.Fatal(err)
			}

			waitSessionIdle(t, session)

			entries, err := session.Entries(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}

			var finished execution.Result
			if err := json.Unmarshal(entries[1].Payload, &finished); err != nil {
				t.Fatal(err)
			}

			want := execution.Completed
			if runErr != nil {
				want = execution.Failed

				if finished.Error != runErr.Error() {
					t.Fatalf("original failure was lost: %+v", finished)
				}
			}

			if finished.Outcome != want || finished.EndedAt.IsZero() || entries[1].ExecutionID != first.ID() || calls.Load() != 2 {
				t.Fatalf("recovered result = %+v, calls = %d", finished, calls.Load())
			}
		})
	}
}

func TestSubmissionRequiresRecoveryBeforeInvokingExecutor(t *testing.T) {
	t.Parallel()

	store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}

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

	session := newExecutorSession(context.Background(), "session-1", store, "work", executor)

	before, err := session.Entries(context.Background(), 0)
	if err != nil || len(before) != 1 {
		t.Fatalf("opening changed entries: %+v, %v", before, err)
	}

	store.failing.Store(true)

	request := execution.Request{Executor: "work"}
	if _, err := session.Execute(context.Background(), request); !errors.Is(err, ErrExecutionUnfinished) {
		t.Fatalf("submission before recovery = %v", err)
	}

	if store.finishes.Load() != 0 {
		t.Fatal("submission attempted recovery")
	}

	if err := session.Recover(context.Background()); !errors.Is(err, store.err) {
		t.Fatalf("recovery persistence error = %v", err)
	}

	if calls.Load() != 0 {
		t.Fatal("executor ran before recovery was durable")
	}

	if _, err := session.Execute(context.Background(), request); !errors.Is(err, ErrBusy) {
		t.Fatalf("repeated submission error = %v", err)
	}

	store.failing.Store(false)

	if err := session.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := session.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	waitSessionIdle(t, session)

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

func TestSubmissionPreservesPreviouslyFinishedExecution(t *testing.T) {
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
	session := newExecutorSession(context.Background(), "session-1", store, "work", executor)

	if err := session.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := session.Execute(context.Background(), execution.Request{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("completed execution changed: %+v, %v", entries, err)
	}
}

func TestExecutionWaitReturnsDurableOutputToIndependentClients(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	release := make(chan struct{})
	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) (any, error) {
		select {
		case <-release:
			return map[string]int{"value": 42}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	session := newExecutorSession(ctx, "session-1", entry.NewMemoryStore(), "work", executor)
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
			result, err := attached.Wait(ctx)
			if err != nil || result.Outcome != execution.Completed || string(result.Output) != `{"value":42}` {
				t.Errorf("client result = %+v, %v", result, err)

				return
			}

			result.Output[0] = 'x'
		})
	}

	close(release)
	clients.Wait()

	result, err := current.Wait(ctx)
	if err != nil || string(result.Output) != `{"value":42}` || result.EndedAt.IsZero() {
		t.Fatalf("result was changed by a client: %+v, %v", result, err)
	}

	entries, err := session.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var stored execution.Result
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &stored); err != nil {
		t.Fatal(err)
	}

	if stored.Outcome != result.Outcome || string(stored.Output) != string(result.Output) || !stored.EndedAt.Equal(result.EndedAt) {
		t.Fatalf("returned result differs from persisted result: %+v, %+v", result, stored)
	}
}

func TestExecutionResultSeparatesWorkFailuresFromWaitErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		output  any
		err     error
		outcome execution.Status
		failure string
	}{
		{name: "completed", output: "done", outcome: execution.Completed},
		{name: "failed with output", output: "partial", err: errors.New("work failed"), outcome: execution.Failed, failure: "work failed"},
		{name: "cancelled", err: context.Canceled, outcome: execution.Cancelled},
		{name: "invalid output", output: make(chan int), outcome: execution.Failed, failure: "encode execution output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
				return test.output, test.err
			})

			session := newExecutorSession(ctx, "session-1", entry.NewMemoryStore(), "work", executor)
			defer session.Close()

			current, err := session.Execute(ctx, execution.Request{Executor: "work"})
			if err != nil {
				t.Fatal(err)
			}

			result, err := current.Wait(ctx)
			if err != nil || result.Outcome != test.outcome || !strings.Contains(result.Error, test.failure) {
				t.Fatalf("execution result = %+v, wait error = %v", result, err)
			}

			if output, ok := test.output.(string); ok {
				var actual string
				if err := json.Unmarshal(result.Output, &actual); err != nil || actual != output {
					t.Fatalf("execution output = %s, %v", result.Output, err)
				}
			}
		})
	}
}

func TestExecutionLookupDoesNotRetryPersistence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}
	store.failing.Store(true)

	var calls atomic.Int32

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		calls.Add(1)

		return map[string]int{"value": 42}, errors.New("work failed")
	})

	session := newExecutorSession(ctx, "session-1", store, "work", executor)
	defer session.Close()

	current, err := session.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, store.err) {
		t.Fatalf("failed persistence was not observable: %v", err)
	}

	if err := session.Cancel(ctx, current.ID()); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("finished work remained cancellable: %v", err)
	}

	store.failing.Store(false)

	current.mu.RLock()
	original := *current.result
	current.mu.RUnlock()

	for range 3 {
		attached, err := session.Execution(ctx, current.ID())
		if err != nil || attached != current {
			t.Fatalf("reattached execution = %v, %v", attached, err)
		}

		if _, err := attached.Wait(ctx); !errors.Is(err, store.err) {
			t.Fatalf("lookup changed persistence error = %v", err)
		}
	}

	if store.finishes.Load() != 1 {
		t.Fatal("lookup retried persistence")
	}

	if err := session.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	result, err := current.Wait(ctx)
	if err != nil || result.Outcome != execution.Failed || result.Error != "work failed" || string(result.Output) != `{"value":42}` {
		t.Fatalf("original result was lost: %+v, %v", result, err)
	}

	if !result.EndedAt.Equal(original.EndedAt) {
		t.Fatal("recovery changed the completion time")
	}

	if calls.Load() != 1 {
		t.Fatal("retrying persistence reran the executor")
	}

	loaded, err := session.Execution(ctx, current.ID())
	if err != nil {
		t.Fatal(err)
	}

	stored, err := loaded.Wait(ctx)
	if err != nil || !stored.EndedAt.Equal(result.EndedAt) || string(stored.Output) != string(result.Output) {
		t.Fatalf("persisted result differs from retained result: %+v, %v", stored, err)
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

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		return "done", nil
	})

	session := newExecutorSession(ctx, "session-1", store, "work", executor)
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

	if result, err := current.Wait(ctx); err != nil || result.Outcome != execution.Completed {
		t.Fatalf("durable completion = %+v, %v", result, err)
	}
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

func TestExecutionLookupLeavesUnfinishedHistoryUntouched(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	if _, err := store.Append(ctx, "session-1",
		entry.Entry{ExecutionID: "old", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`)},
		entry.Entry{ExecutionID: "old", Type: execution.FinishedEntryType, Payload: json.RawMessage(`{"outcome":"completed"}`)},
		entry.Entry{ExecutionID: "unfinished", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`)},
	); err != nil {
		t.Fatal(err)
	}

	session := New(ctx, "session-1", store, execution.NewRegistry())
	defer session.Close()

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

		result, err := old.Wait(ctx)
		if err != nil || result.Outcome != execution.Completed || len(result.Output) != 0 {
			t.Fatalf("legacy result = %+v, %v", result, err)
		}

		entries, err := session.Entries(ctx, 0)
		if err != nil || len(entries) != 3 {
			t.Fatalf("lookup changed history = %+v, %v", entries, err)
		}
	}

	if err := session.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	recovered, err := session.Execution(ctx, "unfinished")
	if err != nil {
		t.Fatal(err)
	}

	result, err := recovered.Wait(ctx)
	if err != nil || result.Outcome != execution.Interrupted {
		t.Fatalf("recovered result = %+v, %v", result, err)
	}

	if err := session.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(ctx, 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("repeated recovery changed history = %+v, %v", entries, err)
	}
}

func TestRecoveryRetainsInterruptedResultAfterPersistenceFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}
	store.failing.Store(true)

	if _, err := store.Append(ctx, "session-1", entry.Entry{
		ExecutionID: "unfinished", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	session := New(ctx, "session-1", store, execution.NewRegistry())
	defer session.Close()

	if err := session.Recover(ctx); !errors.Is(err, store.err) {
		t.Fatalf("recovery persistence error = %v", err)
	}

	current, err := session.Execution(ctx, "unfinished")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, store.err) {
		t.Fatalf("retained recovery persistence error = %v", err)
	}

	current.mu.RLock()
	original := *current.result
	current.mu.RUnlock()

	if err := session.Recover(ctx); !errors.Is(err, store.err) {
		t.Fatalf("repeated recovery persistence error = %v", err)
	}

	store.failing.Store(false)

	attached, err := session.Execution(ctx, current.ID())
	if err != nil || attached != current {
		t.Fatalf("reattachment after storage recovery = %v, %v", attached, err)
	}

	if _, err := attached.Wait(ctx); !errors.Is(err, store.err) {
		t.Fatalf("lookup changed recovery error = %v", err)
	}

	if store.finishes.Load() != 2 {
		t.Fatal("lookup retried recovery")
	}

	if err := session.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	result, err := current.Wait(ctx)
	if err != nil || result.Outcome != execution.Interrupted || !result.EndedAt.Equal(original.EndedAt) {
		t.Fatalf("recovered result = %+v, %v", result, err)
	}

	if err := session.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(ctx, 0)
	if err != nil || len(entries) != 2 || entries[1].ExecutionID != current.ID() || store.finishes.Load() != 3 {
		t.Fatalf("recovery history = %+v, %v", entries, err)
	}
}

func TestRecoverRespectsSessionLifetimeAndCallerContext(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		closed          bool
		lifetimeEnded   bool
		callerCancelled bool
		want            error
	}{
		{name: "empty session"},
		{name: "closed session", closed: true, want: ErrClosed},
		{name: "ended lifetime", lifetimeEnded: true, want: ErrClosed},
		{name: "cancelled caller", callerCancelled: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifetime, endLifetime := context.WithCancel(context.Background())
			defer endLifetime()

			session := New(lifetime, "session-1", entry.NewMemoryStore(), execution.NewRegistry())
			defer session.Close()

			if test.closed {
				session.Close()
			}

			if test.lifetimeEnded {
				endLifetime()
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if test.callerCancelled {
				cancel()
			}

			if err := session.Recover(ctx); !errors.Is(err, test.want) {
				t.Fatalf("Recover() = %v, want %v", err, test.want)
			}
		})
	}
}
