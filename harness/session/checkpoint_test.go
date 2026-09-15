package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestCheckpointCommitsMemoryContext(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("first checkpoint")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 10, OutputTokens: 2},
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("second checkpoint")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 8, OutputTokens: 2},
		}},
	)

	generator, err := checkpoint.NewGenerator(model)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	store := entry.NewMemoryStore()
	session := New(
		State{ID: "session-1"},
		store,
		nil,
		generator,
	)
	recordMessages(t, session, "execution-1", "old input", "old output")

	first, err := session.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("first Checkpoint() error = %v", err)
	}

	if got, want := first.Summary, "first checkpoint"; got != want {
		t.Fatalf("first Summary = %q, want %q", got, want)
	}

	recordMessages(t, session, "execution-2", "new input", "new output")

	second, err := session.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("second Checkpoint() error = %v", err)
	}

	input := model.CallAt(1).Messages[0].TextContent().Text
	if !strings.Contains(input, "user:\nfirst checkpoint") ||
		!strings.Contains(input, "new input") ||
		!strings.Contains(input, "new output") {
		t.Fatalf("second generator input = %q", input)
	}

	if strings.Contains(input, "old input") || strings.Contains(input, "old output") {
		t.Fatalf("second generator input contains checkpointed history: %q", input)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if got, want := len(entries), 6; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}

	if entries[2].Type != checkpoint.EntryType || entries[5].Type != checkpoint.EntryType {
		t.Fatalf("checkpoint entries = %#v, %#v", entries[2], entries[5])
	}

	if entries[5].ExecutionID != "" {
		t.Fatalf("checkpoint ExecutionID = %q, want empty", entries[5].ExecutionID)
	}

	var stored checkpoint.Checkpoint
	if err := json.Unmarshal(entries[5].Payload, &stored); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}

	if stored.Summary != second.Summary {
		t.Fatalf("stored checkpoint = %#v, want %#v", stored, second)
	}
}

func TestCheckpointRequiresContext(t *testing.T) {
	t.Parallel()

	generator, err := checkpoint.NewGenerator(kittest.NewModel(t))
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	session := New(
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		nil,
		generator,
	)
	if _, err := session.Checkpoint(context.Background()); !errors.Is(err, ErrNoCheckpointContext) {
		t.Fatalf("Checkpoint() error = %v, want ErrNoCheckpointContext", err)
	}
}

func TestCheckpointRejectsActiveExecution(t *testing.T) {
	t.Parallel()

	agentModel := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	generator, err := checkpoint.NewGenerator(kittest.NewModel(t))
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	session := New(
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(agentModel),
		generator,
	)

	if _, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("new input"))); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	<-agentModel.started

	if _, err := session.Checkpoint(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("Checkpoint() error = %v, want ErrBusy", err)
	}

	close(agentModel.release)
	waitSessionIdle(t, session)
}

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
	session := New(
		State{ID: "session-1"},
		store,
		agent.New(agentModel, agent.WithContributors(automatic.New())),
		generator,
	)

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

func recordMessages(
	t *testing.T,
	session *Session,
	executionID, input, output string,
) {
	t.Helper()

	if err := session.Commit(
		context.Background(),
		executionID,
		"message",
		kit.NewUserMessage(kit.NewTextContent(input)),
		kit.NewModelMessage(kit.NewTextContent(output)),
	); err != nil {
		t.Fatalf("commit messages: %v", err)
	}
}
