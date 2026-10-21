package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

func TestRunReconcilesToolsBeforeCancellation(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()
	executor := newExecutor(store, nil, nil)

	executionID := "execution-1"
	call := kit.NewToolCall("call-1", "write", nil)

	if err := executor.events.Commit(
		context.Background(),
		executionID,
		StartedEntryType,
		Started{},
	); err != nil {
		t.Fatalf("commit execution start: %v", err)
	}

	if err := executor.events.Commit(
		context.Background(),
		executionID,
		"message",
		kit.NewModelMessage(kit.NewToolCallContent(call)),
	); err != nil {
		t.Fatalf("commit tool call: %v", err)
	}

	executor.run(
		context.Background(),
		&Execution{id: executionID, cancel: func() {}, done: make(chan struct{})},
		interruptedAgent{},
		kit.Message{},
	)

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
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

	var finished Finished
	if err := json.Unmarshal(entries[3].Payload, &finished); err != nil {
		t.Fatalf("decode finished execution: %v", err)
	}

	if finished.Outcome != Cancelled {
		t.Fatalf("execution outcome = %q, want %q", finished.Outcome, Cancelled)
	}
}

func newExecutor(
	store entry.Store,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) *Executor {
	return NewExecutor("session-1", store, event.NewBus("session-1", store), agents, checkpoints)
}

func waitDone(t *testing.T, execution *Execution) {
	t.Helper()

	select {
	case <-execution.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for execution to finish")
	}
}

func receiveEvent(t *testing.T, subscription *event.Subscription) event.Event {
	t.Helper()

	select {
	case value, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")

		return event.Event{}
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
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
