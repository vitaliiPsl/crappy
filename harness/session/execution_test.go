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

	session := newAgentSession(context.Background(), t,
		"session-1",
		store,
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	firstInput := kit.NewUserMessage(kit.NewTextContent("first input"))

	firstExecution, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: firstInput})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	waitExecution(t, firstExecution)

	secondExecution, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("second input"))})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	waitExecution(t, secondExecution)

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

	session := newAgentSession(context.Background(), t,
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

	close(model.release)
	waitExecution(t, current)
}

func TestExecuteRecordsFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("model failed")
	model := kittest.NewModel(t, kittest.ModelResult{Error: wantErr})

	session := newAgentSession(context.Background(), t,
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
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

	session := newAgentSession(context.Background(), t,
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

	waitExecution(t, current)

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

	session := newAgentSession(ctx, t,
		"session-1",
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}))

	current, err := session.Execute(context.Background(), execution.Request{Executor: agent.ExecutorName, Input: kit.NewUserMessage(kit.NewTextContent("input"))})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started
	cancel()
	waitExecution(t, current)

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

	session := newExecutorSession(context.Background(), t, "session-1", entry.NewMemoryStore(), "work", executor)

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

			session := newExecutorSession(ctx, t, "session-1", entry.NewMemoryStore(), "work", executor)
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

	store := newFinishFailingStore(errors.New("storage unavailable"))
	store.failing.Store(true)

	var calls atomic.Int32

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		calls.Add(1)

		return map[string]int{"value": 42}, errors.New("work failed")
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

	executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		return "done", nil
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

	if result, err := current.Wait(ctx); err != nil || result.Outcome != execution.Completed {
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
		entry.Entry{ExecutionID: "old", Type: execution.FinishedEntryType, Payload: json.RawMessage(`{"outcome":"completed"}`)},
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

		result, err := old.Wait(ctx)
		if err != nil || result.Outcome != execution.Completed || len(result.Output) != 0 {
			t.Fatalf("legacy result = %+v, %v", result, err)
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

			executor := execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
				calls.Add(1)

				return nil, runErr
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
		func(context.Context, execution.ExecuteParams) (any, error) { return nil, nil },
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

func TestExecutionRetainsResultWhenFinishCannotBeCommitted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := execution.NewRegistry()
	opened := newSession(ctx, t, "session", entry.NewMemoryStore(), registry)

	workErr := errors.New("work failed")
	registry.Register("work", execution.Func(func(context.Context, execution.ExecuteParams) (any, error) {
		opened.events.Close()

		return map[string]int{"value": 42}, workErr
	}))

	current, err := opened.Execute(ctx, execution.Request{Executor: "work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := current.Wait(ctx); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("finish error = %v", err)
	}

	current.mu.RLock()
	original := *current.result
	current.mu.RUnlock()

	for range 3 {
		if _, err := opened.Execution(ctx, current.ID()); !errors.Is(err, ErrExecutionUnfinished) {
			t.Fatalf("uncommitted execution lookup = %v", err)
		}

		if _, err := current.Wait(ctx); !errors.Is(err, event.ErrClosed) {
			t.Fatalf("finish error changed = %v", err)
		}
	}

	current.mu.RLock()
	retained := *current.result
	current.mu.RUnlock()

	if retained.Outcome != execution.Failed || retained.Error != workErr.Error() ||
		!retained.EndedAt.Equal(original.EndedAt) || string(retained.Output) != `{"value":42}` {
		t.Fatalf("execution result changed after failed commit: %+v", retained)
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

type blockingModel struct {
	started chan struct{}
	release chan struct{}
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
