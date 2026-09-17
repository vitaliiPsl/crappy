package agent

import (
	"context"
	"errors"
	"testing"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
	"github.com/vitaliiPsl/crappy-adk/x/memory"
)

func TestFactoryBuild(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	baseTool := kittest.NewTool(t, "base", "Base capability")
	factory := New(
		staticModelResolver{model},
		WithTools(baseTool),
		WithAgentOptions(adkagent.WithInstructions("Be concise.")),
	)

	built, err := factory.Build(context.Background(), BuildRequest{
		Memory: memory.NewHistory(),
	})
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

	if got, want := request.Tools, []kit.Tool{baseTool}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Tools = %v, want %v", got, want)
	}
}

func TestFactoryBuildsFreshAgents(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	factory := New(staticModelResolver{model})

	first, err := factory.Build(context.Background(), BuildRequest{
		Memory: memory.NewHistory(),
	})
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}

	second, err := factory.Build(context.Background(), BuildRequest{
		Memory: memory.NewHistory(),
	})
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}

	if first == second {
		t.Fatal("Build() returned the same Agent twice")
	}
}

func TestFactoryBuildAppliesContributions(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	history := memory.NewHistory()
	contributedTool := kittest.NewTool(t, "search", "Search for information")

	type contextKey struct{}

	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	contributor := ContributorFunc(func(
		ctx context.Context,
		build BuildContext,
	) (Contribution, error) {
		if got, want := ctx.Value(contextKey{}), "value"; got != want {
			t.Fatalf("context value = %v, want %v", got, want)
		}

		if build.Request.Memory != history {
			t.Fatal("BuildContext.Request.Memory does not contain the build memory")
		}

		if got, want := build.Request.SessionID, "session-1"; got != want {
			t.Fatalf("BuildContext.Request.SessionID = %q, want %q", got, want)
		}

		if got, want := build.Request.ExecutionID, "execution-1"; got != want {
			t.Fatalf("BuildContext.Request.ExecutionID = %q, want %q", got, want)
		}

		if build.Model != model {
			t.Fatal("BuildContext.Model does not contain the factory model")
		}

		return Contribution{
			Instructions: []string{"Use tools when useful."},
			Tools:        []kit.Tool{contributedTool},
			Options:      []adkagent.Option{adkagent.WithTemperature(0.5)},
		}, nil
	})
	factory := New(
		staticModelResolver{model},
		WithAgentOptions(adkagent.WithInstructions("Be concise.")),
		WithContributors(contributor),
	)

	built, err := factory.Build(ctx, BuildRequest{
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Memory:      history,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	_, err = built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("Hi")))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request := model.CallAt(0)
	if got, want := request.Instructions, "Be concise.\n\nUse tools when useful."; got != want {
		t.Fatalf("Instructions = %q, want %q", got, want)
	}

	if got, want := request.Tools, []kit.Tool{contributedTool}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Tools = %v, want %v", got, want)
	}

	if request.Config.Temperature == nil || *request.Config.Temperature != 0.5 {
		t.Fatalf("Temperature = %v, want 0.5", request.Config.Temperature)
	}
}

func TestFactoryBuildReturnsContributorError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("contribution failed")
	factory := New(
		staticModelResolver{kittest.NewModel(t)},
		WithContributors(
			ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
				return Contribution{}, wantErr
			}),
		),
	)

	_, err := factory.Build(context.Background(), BuildRequest{
		Memory: memory.NewHistory(),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Build() error = %v, want %v", err, wantErr)
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
