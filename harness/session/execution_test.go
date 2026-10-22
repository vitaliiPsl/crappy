package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	firstInput := kit.NewUserMessage(kit.NewTextContent("first input"))

	firstID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: firstInput})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	secondID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("second input"))})
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
		execution.AgentFinishedEntryType,
		execution.FinishedEntryType,
		execution.StartedEntryType,
		"message",
		"message",
		execution.AgentFinishedEntryType,
		execution.FinishedEntryType,
	}
	for i, wantType := range wantTypes {
		if entries[i].Type != wantType {
			t.Fatalf("entry %d type = %q, want %q", i, entries[i].Type, wantType)
		}
	}

	for i, value := range entries[:5] {
		if value.ExecutionID != firstID {
			t.Fatalf("first execution entry %d ID = %q, want %q", i, value.ExecutionID, firstID)
		}
	}

	for i, value := range entries[5:] {
		if value.ExecutionID != secondID {
			t.Fatalf("second execution entry %d ID = %q, want %q", i, value.ExecutionID, secondID)
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

	var firstFinished execution.Finished
	if err := json.Unmarshal(entries[4].Payload, &firstFinished); err != nil {
		t.Fatalf("decode first finished execution: %v", err)
	}

	if got, want := firstFinished.Outcome, execution.Completed; got != want {
		t.Fatalf("first execution outcome = %q, want %q", got, want)
	}

	var firstResult execution.AgentFinished
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

	var secondFinished execution.AgentFinished
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	executionID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("first input"))})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if len(entries) == 0 ||
		entries[0].ExecutionID != executionID ||
		entries[0].Type != execution.StartedEntryType {
		t.Fatalf("first entry = %#v, want started execution %q", entries, executionID)
	}

	<-model.started

	_, err = session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("second input"))})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Execute() error = %v, want ErrBusy", err)
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	executionID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	last := entries[len(entries)-1]
	if last.ExecutionID != executionID || last.Type != execution.FinishedEntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, executionID)
	}

	var finished execution.Finished
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	executionID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started

	if err := session.Cancel(context.Background(), "other-execution"); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(other) error = %v, want ErrExecutionNotActive", err)
	}

	if err := session.Cancel(context.Background(), executionID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Finished
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, execution.Cancelled; got != want {
		t.Fatalf("execution outcome = %q, want %q", got, want)
	}

	if err := session.Cancel(context.Background(), executionID); !errors.Is(err, ErrExecutionNotActive) {
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	if _, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("input"))}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started
	cancel()
	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished execution.Finished
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
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil)

	session.Close()
	session.Close()

	_, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
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
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil)

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	executionID, err := session.Execute(context.Background(), execution.Request{Executor: execution.AgentName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
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
	if last.ExecutionID != executionID || last.Type != execution.FinishedEntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, executionID)
	}

	var finished execution.Finished
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
		idle := session.activeID == ""
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
	err     error
	failing atomic.Bool
}

func (s *finishFailingStore) Append(
	ctx context.Context,
	sessionID string,
	entries ...entry.Entry,
) ([]entry.Entry, error) {
	for _, current := range entries {
		if current.Type == execution.FinishedEntryType && s.failing.Load() {
			return nil, s.err
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

	session := New(context.Background(),
		"session-1",
		store,
		WithExecutors(map[string]execution.Executor{
			"function": execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
				started <- params

				<-release

				return params.Events.Commit(ctx, params.ExecutionID, "function.output", params.Input)
			}),
		}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := map[string]int{"value": 1}

	value, err := session.Execute(ctx, execution.Request{Executor: "function", Input: input})
	if err != nil {
		t.Fatal(err)
	}

	params := <-started
	if params.SessionID != session.ID() || params.ExecutionID != value || params.Entries != store {
		t.Fatalf("executor parameters = %+v", params)
	}

	if value == "" {
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

	var finished execution.Finished
	if err := json.Unmarshal(entries[2].Payload, &finished); err != nil {
		t.Fatal(err)
	}

	if finished.Outcome != execution.Completed {
		t.Fatalf("client cancellation affected invocation: %+v", finished)
	}
}

func TestExecuteValidatesBeforeRecording(t *testing.T) {
	t.Parallel()

	session := New(context.Background(),
		"session-1",
		entry.NewMemoryStore(),
		WithExecutors(map[string]execution.Executor{
			"work": execution.Func(func(context.Context, execution.ExecuteParams) error {
				t.Error("invalid request reached executor")

				return nil
			}),
		}))

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
			store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}
			store.failing.Store(true)

			var calls atomic.Int32

			session := New(context.Background(),
				"session-1",
				store,
				WithExecutors(map[string]execution.Executor{
					"work": execution.Func(func(context.Context, execution.ExecuteParams) error {
						calls.Add(1)

						return runErr
					}),
				}))

			request := execution.Request{Executor: "work"}

			first, err := session.Execute(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}

			waitInvocation(t, session)

			if err := session.Cancel(context.Background(), first); !errors.Is(err, ErrExecutionNotActive) {
				t.Fatalf("cancel after invocation returned = %v", err)
			}

			if _, err := session.Execute(context.Background(), request); !errors.Is(err, store.err) {
				t.Fatalf("submission after failed commit = %v", err)
			}

			store.failing.Store(false)

			if _, err := session.Execute(context.Background(), request); err != nil {
				t.Fatal(err)
			}

			waitSessionIdle(t, session)

			entries, err := session.Entries(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}

			var finished execution.Finished
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

			if finished.Outcome != want || finished.EndedAt.IsZero() || entries[1].ExecutionID != first || calls.Load() != 2 {
				t.Fatalf("recovered result = %+v, calls = %d", finished, calls.Load())
			}
		})
	}
}

func waitInvocation(t *testing.T, session *Session) {
	t.Helper()
	session.mu.Lock()
	done := session.done
	session.mu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("invocation did not finish")
	}
}

func TestSubmissionRecoversUnfinishedExecutionBeforeInvokingExecutor(t *testing.T) {
	t.Parallel()

	store := &finishFailingStore{Store: entry.NewMemoryStore(), err: errors.New("storage unavailable")}

	bus := event.NewBus("session-1", store)
	if err := bus.Commit(context.Background(), "unfinished", execution.StartedEntryType, execution.Started{
		Executor: "removed-executor", Input: json.RawMessage(`{"value":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32

	session := New(context.Background(),
		"session-1",
		store,
		WithExecutors(map[string]execution.Executor{
			"work": execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
				calls.Add(1)

				entries, err := params.Entries.List(ctx, params.SessionID, 0)
				if err != nil {
					return err
				}

				if len(entries) != 3 || entries[1].Type != execution.FinishedEntryType {
					t.Errorf("new work started before recovery: %+v", entries)
				}

				return nil
			}),
		}))

	before, err := session.Entries(context.Background(), 0)
	if err != nil || len(before) != 1 {
		t.Fatalf("opening changed entries: %+v, %v", before, err)
	}

	store.failing.Store(true)

	request := execution.Request{Executor: "work"}
	if _, err := session.Execute(context.Background(), request); !errors.Is(err, store.err) {
		t.Fatalf("recovery persistence error = %v", err)
	}

	if calls.Load() != 0 {
		t.Fatal("executor ran before recovery was durable")
	}

	if _, err := session.Execute(context.Background(), request); !errors.Is(err, store.err) {
		t.Fatalf("repeated submission error = %v", err)
	}

	store.failing.Store(false)

	if _, err := session.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("recovered journal = %+v, %v", entries, err)
	}

	var previous execution.Finished
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

	if err := bus.Commit(context.Background(), "completed", execution.FinishedEntryType, execution.Finished{Outcome: execution.Completed}); err != nil {
		t.Fatal(err)
	}

	session := New(context.Background(),
		"session-1",
		store,
		WithExecutors(map[string]execution.Executor{
			"work": execution.Func(func(context.Context, execution.ExecuteParams) error { return nil }),
		}))

	if _, err := session.Execute(context.Background(), execution.Request{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("completed execution changed: %+v, %v", entries, err)
	}
}
