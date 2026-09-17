package automatic

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	harnessagent "github.com/vitaliipsl/crappy/harness/agent"
)

func TestContributorCheckpointsBeforeModelRequest(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	memory := &testMemory{}
	checkpointed := 0
	factory := harnessagent.New(
		kit.AgentConfig{},
		staticModelResolver{model},
		harnessagent.WithContributors(New(
			WithEstimator(fixedEstimator{request: 80}),
		)),
	)

	built, err := factory.Build(context.Background(), harnessagent.BuildRequest{
		Memory: memory,
		Checkpoint: func(context.Context) error {
			checkpointed++
			memory.context = []kit.Message{
				kit.NewUserMessage(kit.NewTextContent("checkpoint")),
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	_, err = built.Run(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("original")),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if checkpointed != 1 {
		t.Fatalf("checkpoint calls = %d, want 1", checkpointed)
	}

	messages := model.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, "checkpoint"; got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}
}

func TestContributorUsesLastModelUsage(t *testing.T) {
	t.Parallel()

	contributor := New(WithEstimator(fixedEstimator{request: 1000, message: 7}))
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

type testMemory struct {
	context []kit.Message
	history []kit.Message
}

func (m *testMemory) Context(context.Context) ([]kit.Message, error) {
	return append([]kit.Message(nil), m.context...), nil
}

func (m *testMemory) History(context.Context) ([]kit.Message, error) {
	return append([]kit.Message(nil), m.history...), nil
}

func (m *testMemory) Record(_ context.Context, messages ...kit.Message) error {
	m.context = append(m.context, messages...)
	m.history = append(m.history, messages...)

	return nil
}
