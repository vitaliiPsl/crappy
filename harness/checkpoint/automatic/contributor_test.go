package automatic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	harnessagent "github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func TestContributorCheckpointsBeforeModelRequest(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 10, OutputTokens: 2},
		}},
	)
	checkpointModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"checkpoint"}`)),
		FinishReason: kit.FinishReasonStop,
		Usage:        kit.Usage{InputTokens: 50, OutputTokens: 5},
	}})
	built := buildAgent(t, model, checkpointModel, WithEstimator(fixedEstimator{request: 80}))

	response, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original")))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := response.Usage, (kit.Usage{InputTokens: 60, OutputTokens: 7}); got != want {
		t.Fatalf("response usage = %#v, want %#v", got, want)
	}

	if got, want := response.LastUsage, (kit.Usage{InputTokens: 10, OutputTokens: 2}); got != want {
		t.Fatalf("response last usage = %#v, want %#v", got, want)
	}

	checkpointModel.AssertCallCount(t, 1)

	messages := model.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, checkpointMessage("checkpoint"); got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}
}

func TestContributorCheckpointsAndRetriesOnContextOverflow(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Error: kit.ErrContextLength},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	checkpointModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"checkpoint"}`)),
		FinishReason: kit.FinishReasonStop,
	}})
	built := buildAgent(t, model, checkpointModel, WithEstimator(fixedEstimator{}))

	if _, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original"))); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	checkpointModel.AssertCallCount(t, 1)
	model.AssertCallCount(t, 2)

	if got, want := model.CallAt(1).Messages[0].TextContent().Text, checkpointMessage("checkpoint"); got != want {
		t.Fatalf("retried request message = %q, want %q", got, want)
	}
}

func TestContributorIgnoresOtherModelErrors(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Error: kit.ErrRateLimit},
	)
	checkpointModel := kittest.NewModel(t)
	built := buildAgent(t, model, checkpointModel, WithEstimator(fixedEstimator{}))

	_, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original")))
	if !errors.Is(err, kit.ErrRateLimit) {
		t.Fatalf("Run() error = %v, want ErrRateLimit", err)
	}

	model.AssertCallCount(t, 1)
	checkpointModel.AssertCallCount(t, 0)
}

func buildAgent(t *testing.T, model, checkpointModel kit.Model, options ...ContributorOption) kit.Agent {
	t.Helper()

	generator, err := checkpoint.NewGenerator(checkpoint.Config{}, staticModelResolver{checkpointModel})
	if err != nil {
		t.Fatal(err)
	}

	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	factory := harnessagent.New(
		kit.AgentConfig{},
		staticModelResolver{model},
		harnessagent.WithContributors(New(generator, options...)),
	)

	built, err := factory.Build(context.Background(), harnessagent.BuildRequest{
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Memory:      memory.New("session-1", "execution-1", store, bus),
		Entries:     store,
		Events:      bus,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	return built
}

func TestContributorUsesLastModelUsage(t *testing.T) {
	t.Parallel()

	contributor := New(nil, WithEstimator(fixedEstimator{request: 1000, message: 7}))
	run := &kit.RunContext{
		LastUsage: kit.Usage{InputTokens: 100, OutputTokens: 20},
		Messages: []kit.Message{
			kit.NewModelMessage(kit.NewTextContent("tool call")),
			kit.NewToolMessage(kit.NewTextContent("tool result")),
		},
	}

	if got, want := contributor.inputTokens(run, kit.ModelRequest{}), int64(127); got != want {
		t.Fatalf("inputTokens() = %d, want %d", got, want)
	}
}

type fixedEstimator struct {
	request int
	message int
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

func (e fixedEstimator) EstimateRequest(kit.ModelRequest) int {
	return e.request
}

func (e fixedEstimator) EstimateMessage(kit.Message) int {
	return e.message
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
	executor := harnessagent.NewExecutor(harnessagent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, harnessagent.WithContributors(New(generator)),
	))

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	executionID := "execution-1"
	done := make(chan error, 1)

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: executionID, Input: agentInput(t, "start"), Entries: store, Events: bus}
	go func() {
		_, err := executor.Execute(context.Background(), params)
		done <- err
	}()

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

	executor := harnessagent.NewExecutor(harnessagent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, harnessagent.WithContributors(New(generator)),
	))
	if _, err := executor.Execute(context.Background(), execution.ExecuteParams{
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

func receiveEvent(t *testing.T, subscription *event.Subscription) event.Event {
	t.Helper()

	select {
	case value, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")

		return event.Event{}
	}
}

func agentInput(t *testing.T, text string) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(text)))
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}
