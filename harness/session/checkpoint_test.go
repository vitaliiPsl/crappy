package session

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/harness/entry"
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
		Message:      kit.NewModelMessage(kit.NewTextContent("checkpointed context")),
		FinishReason: kit.FinishReasonStop,
	}})

	generator, err := checkpoint.NewGenerator(checkpointModel)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	store := entry.NewMemoryStore()

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		store,
		agent.New(agentModel, agent.WithContributors(automatic.New())),
		generator,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	executionID, err := session.Execute(
		kit.NewUserMessage(kit.NewTextContent("start")),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	waitSessionIdle(t, session)

	messages := agentModel.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, "checkpointed context"; got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(entries) < 3 {
		t.Fatalf("entries = %d, want at least 3", len(entries))
	}

	if got, want := entries[2].Type, checkpoint.EntryType; got != want {
		t.Fatalf("entry 3 type = %q, want %q", got, want)
	}

	if got := entries[2].ExecutionID; got != executionID {
		t.Fatalf("checkpoint ExecutionID = %q, want %q", got, executionID)
	}
}
