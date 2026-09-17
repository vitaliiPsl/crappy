package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
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

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		store,
		agent.New(model),
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	firstInput := kit.NewUserMessage(kit.NewTextContent("first input"))

	firstID, err := session.Execute(firstInput)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	secondID, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("second input")))
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

	if got, want := len(entries), 8; got != want {
		t.Fatalf("Entries() returned %d entries, want %d", got, want)
	}

	wantTypes := []string{
		ExecutionStartedEntryType,
		"message",
		"message",
		ExecutionFinishedEntryType,
		ExecutionStartedEntryType,
		"message",
		"message",
		ExecutionFinishedEntryType,
	}
	for i, wantType := range wantTypes {
		if entries[i].Type != wantType {
			t.Fatalf("entry %d type = %q, want %q", i, entries[i].Type, wantType)
		}
	}

	for i, value := range entries[:4] {
		if value.ExecutionID != firstID {
			t.Fatalf("first execution entry %d ID = %q, want %q", i, value.ExecutionID, firstID)
		}
	}

	for i, value := range entries[4:] {
		if value.ExecutionID != secondID {
			t.Fatalf("second execution entry %d ID = %q, want %q", i, value.ExecutionID, secondID)
		}
	}

	var firstStarted ExecutionStarted
	if err := json.Unmarshal(entries[0].Payload, &firstStarted); err != nil {
		t.Fatalf("decode first started execution: %v", err)
	}

	if got, want := firstStarted.Input.Content[0].Text.Text, "first input"; got != want {
		t.Fatalf("first execution input = %q, want %q", got, want)
	}

	var firstOutput kit.Message
	if err := json.Unmarshal(entries[2].Payload, &firstOutput); err != nil {
		t.Fatalf("decode first output: %v", err)
	}

	if got, want := firstOutput.Content[0].Text.Text, "first response"; got != want {
		t.Fatalf("first output = %q, want %q", got, want)
	}

	var firstFinished ExecutionFinished
	if err := json.Unmarshal(entries[3].Payload, &firstFinished); err != nil {
		t.Fatalf("decode first finished execution: %v", err)
	}

	if got, want := firstFinished.Outcome, ExecutionCompleted; got != want {
		t.Fatalf("first execution outcome = %q, want %q", got, want)
	}

	if got, want := firstFinished.Usage.InputTokens, int64(10); got != want {
		t.Fatalf("first result input tokens = %d, want %d", got, want)
	}

	if got, want := firstFinished.LastUsage.OutputTokens, int64(2); got != want {
		t.Fatalf("first result last output tokens = %d, want %d", got, want)
	}

	var secondOutput kit.Message
	if err := json.Unmarshal(entries[6].Payload, &secondOutput); err != nil {
		t.Fatalf("decode second output: %v", err)
	}

	if got, want := secondOutput.Content[0].Text.Text, "second response"; got != want {
		t.Fatalf("second output = %q, want %q", got, want)
	}

	var secondFinished ExecutionFinished
	if err := json.Unmarshal(entries[7].Payload, &secondFinished); err != nil {
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

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(model),
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	executionID, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("first input")))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if len(entries) == 0 ||
		entries[0].ExecutionID != executionID ||
		entries[0].Type != ExecutionStartedEntryType {
		t.Fatalf("first entry = %#v, want started execution %q", entries, executionID)
	}

	<-model.started

	_, err = session.Execute(kit.NewUserMessage(kit.NewTextContent("second input")))
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

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(model),
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	executionID, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("input")))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	last := entries[len(entries)-1]
	if last.ExecutionID != executionID || last.Type != ExecutionFinishedEntryType {
		t.Fatalf("last entry = %#v, want finished execution %q", last, executionID)
	}

	var finished ExecutionFinished
	if err := json.Unmarshal(last.Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, ExecutionFailed; got != want {
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

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(model),
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	executionID, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("input")))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-model.started

	if err := session.Cancel("other-execution"); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(other) error = %v, want ErrExecutionNotActive", err)
	}

	if err := session.Cancel(executionID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	waitSessionIdle(t, session)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	var finished ExecutionFinished
	if err := json.Unmarshal(entries[len(entries)-1].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if got, want := finished.Outcome, ExecutionCancelled; got != want {
		t.Fatalf("execution outcome = %q, want %q", got, want)
	}

	if err := session.Cancel(executionID); !errors.Is(err, ErrExecutionNotActive) {
		t.Fatalf("Cancel(completed) error = %v, want ErrExecutionNotActive", err)
	}
}

func TestRunExecutionReconcilesToolsBeforeCancellation(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	session, err := New(context.Background(), State{ID: "session-1"}, store, nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	executionID := "execution-1"
	call := kit.NewToolCall("call-1", "write", nil)

	if err := session.Commit(
		context.Background(),
		executionID,
		ExecutionStartedEntryType,
		ExecutionStarted{},
	); err != nil {
		t.Fatalf("commit execution start: %v", err)
	}

	if err := session.Commit(
		context.Background(),
		executionID,
		"message",
		kit.NewModelMessage(kit.NewToolCallContent(call)),
	); err != nil {
		t.Fatalf("commit tool call: %v", err)
	}

	session.runExecution(
		context.Background(),
		&execution{id: executionID},
		interruptedAgent{},
		kit.Message{},
	)

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if got, want := len(entries), 4; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}

	var message kit.Message
	if err := json.Unmarshal(entries[2].Payload, &message); err != nil {
		t.Fatalf("decode reconciled message: %v", err)
	}

	results := message.ToolResults()
	if len(results) != 1 || results[0].Call.ID != call.ID {
		t.Fatalf("reconciled results = %+v, want interrupted tool result", results)
	}

	var finished ExecutionFinished
	if err := json.Unmarshal(entries[3].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Outcome != ExecutionCancelled {
		t.Fatalf("execution outcome = %q, want %q", finished.Outcome, ExecutionCancelled)
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
		idle := session.active == nil
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

type interruptedAgent struct{}

func (interruptedAgent) Run(context.Context, kit.Message) (kit.AgentResponse, error) {
	return kit.AgentResponse{}, context.Canceled
}

func (interruptedAgent) Stream(context.Context, kit.Message) *kit.Stream[kit.AgentEvent, kit.AgentResponse] {
	return kit.NewStream(func(kit.Emitter[kit.AgentEvent]) (kit.AgentResponse, error) {
		return kit.AgentResponse{}, context.Canceled
	})
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
