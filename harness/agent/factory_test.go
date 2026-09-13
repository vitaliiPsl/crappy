package agent

import (
	"context"
	"testing"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
	"github.com/vitaliiPsl/crappy-adk/x/memory"
)

type toolSet struct{}

func (toolSet) Add(...kit.Tool) {}

func (toolSet) Get(string) (kit.Tool, bool) {
	return nil, false
}

func (toolSet) List() []kit.Tool {
	return nil
}

func TestFactoryBuild(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	factory := New(
		model,
		toolSet{},
		adkagent.WithInstructions("Be concise."),
	)

	built, err := factory.Build(memory.NewHistory())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	_, err = built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("Hi")))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request := model.CallAt(0)
	if got, want := request.Instructions, "Be concise."; got != want {
		t.Fatalf("Instructions = %q, want %q", got, want)
	}
}

func TestFactoryBuildsFreshAgents(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	factory := New(model, toolSet{})

	first, err := factory.Build(memory.NewHistory())
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}

	second, err := factory.Build(memory.NewHistory())
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}

	if first == second {
		t.Fatal("Build() returned the same Agent twice")
	}
}
