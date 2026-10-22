package execution

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func TestAutomaticCheckpointDuringExecution(t *testing.T) {
	t.Parallel()

	agentModel := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 1},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	checkpointModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective": "checkpointed context", "progress": [], "context": [], "next": []}`)),
		FinishReason: kit.FinishReasonStop,
	}})

	generator, err := checkpoint.NewGenerator(
		checkpoint.Config{},
		staticModelResolver{checkpointModel},
	)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	executor := NewAgent(agent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, agent.WithContributors(automatic.New()),
	), WithCheckpoints(generator))

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	executionID := "execution-1"
	done := make(chan error, 1)

	params := ExecuteParams{SessionID: "session-1", ExecutionID: executionID, Input: agentInput(t, "start"), Entries: store, Events: bus}
	go func() { done <- executor.Execute(context.Background(), params) }()

	started := false
	for {
		received := receiveEvent(t, subscription)
		if received.Type == checkpoint.StartedEventType {
			started = received.ExecutionID == executionID
		}

		if received.Type == checkpoint.EntryType {
			break
		}
	}

	if !started {
		t.Fatal("checkpoint started event was not emitted before the checkpoint entry")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	messages := agentModel.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, checkpointMessage("checkpointed context"); got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(entries) < 3 {
		t.Fatalf("entries = %d, want at least 3", len(entries))
	}

	if got, want := entries[1].Type, checkpoint.EntryType; got != want {
		t.Fatalf("entry 3 type = %q, want %q", got, want)
	}

	if got := entries[1].ExecutionID; got != executionID {
		t.Fatalf("checkpoint ExecutionID = %q, want %q", got, executionID)
	}
}

func TestAutomaticCheckpointRetainsCurrentInput(t *testing.T) {
	t.Parallel()

	agentModel := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 1},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	checkpointModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective": "checkpointed context", "progress": [], "context": [], "next": []}`)),
		FinishReason: kit.FinishReasonStop,
	}})

	generator, err := checkpoint.NewGenerator(checkpoint.Config{}, staticModelResolver{checkpointModel})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	store := entry.NewMemoryStore()
	if err := memory.New("session-1", "", store, event.NewBus("session-1", store)).Record(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("earlier")),
		kit.NewModelMessage(kit.NewTextContent("earlier response")),
	); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	executor := NewAgent(agent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, agent.WithContributors(automatic.New()),
	), WithCheckpoints(generator))
	if err := executor.Execute(context.Background(), ExecuteParams{
		SessionID: "session-1", ExecutionID: "execution-1", Input: agentInput(t, "current"), Entries: store, Events: event.NewBus("session-1", store),
	}); err != nil {
		t.Fatal(err)
	}

	messages := agentModel.CallAt(0).Messages
	want := []string{checkpointMessage("checkpointed context"), "earlier response", "current"}

	if got := len(messages); got != len(want) {
		t.Fatalf("request messages = %d, want %d", got, len(want))
	}

	for i, text := range want {
		if got := messages[i].TextContent().Text; got != text {
			t.Fatalf("request message %d = %q, want %q", i, got, text)
		}
	}
}

func checkpointMessage(objective string) string {
	return checkpoint.Checkpoint{Summary: checkpoint.Summary{Objective: objective}}.Message().TextContent().Text
}
