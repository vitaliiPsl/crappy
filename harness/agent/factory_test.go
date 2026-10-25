package agent

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
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
	temperature := float32(0.3)
	maxOutputTokens := int32(1024)
	thinking := kit.ThinkingLevelHigh
	factory := New(
		kit.AgentConfig{
			Instructions:    "Be concise.",
			Temperature:     &temperature,
			MaxOutputTokens: &maxOutputTokens,
			Thinking:        &thinking,
		},
		staticModelResolver{model},
		WithTools(baseTool),
	)

	built, err := factory.Build(context.Background(), testBuildRequest())
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

	if request.Config.Temperature == nil || *request.Config.Temperature != temperature {
		t.Fatalf("Temperature = %v, want %v", request.Config.Temperature, temperature)
	}

	if request.Config.MaxOutputTokens == nil || *request.Config.MaxOutputTokens != maxOutputTokens {
		t.Fatalf("MaxOutputTokens = %v, want %v", request.Config.MaxOutputTokens, maxOutputTokens)
	}

	if request.Config.Thinking == nil || *request.Config.Thinking != thinking {
		t.Fatalf("Thinking = %v, want %v", request.Config.Thinking, thinking)
	}

	if got, want := request.Tools, []kit.Tool{baseTool}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Tools = %v, want %v", got, want)
	}
}

func TestFactoryBuildsFreshAgents(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	factory := New(kit.AgentConfig{}, staticModelResolver{model})

	first, err := factory.Build(context.Background(), testBuildRequest())
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}

	second, err := factory.Build(context.Background(), testBuildRequest())
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
	buildRequest := testBuildRequest()
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

		if build.Request.Memory != buildRequest.Memory || build.Request.Entries != buildRequest.Entries || build.Request.Events != buildRequest.Events {
			t.Fatal("BuildContext does not contain the supplied history capabilities")
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
			Options: []adkagent.Option{
				adkagent.WithTemperature(0.5),
				adkagent.WithOnTurnStart(func(run *kit.RunContext) error {
					if run.Memory != build.Request.Memory {
						t.Fatal("agent did not use the supplied memory")
					}

					return nil
				}),
			},
		}, nil
	})
	factory := New(
		kit.AgentConfig{},
		staticModelResolver{model},
		WithAgentOptions(adkagent.WithInstructions("Be concise.")),
		WithContributors(contributor),
	)

	built, err := factory.Build(ctx, buildRequest)
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
		kit.AgentConfig{},
		staticModelResolver{kittest.NewModel(t)},
		WithContributors(
			ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
				return Contribution{}, wantErr
			}),
		),
	)

	_, err := factory.Build(context.Background(), testBuildRequest())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Build() error = %v, want %v", err, wantErr)
	}
}

func TestFactoryAppliesToolMiddleware(t *testing.T) {
	t.Parallel()

	base := kittest.NewTool(t, "base", "Base capability")
	contributed := kittest.NewTool(t, "contributed", "Contributed capability")

	var wrapped []string

	factory := New(
		kit.AgentConfig{},
		staticModelResolver{kittest.NewModel(t)},
		WithTools(base),
		WithContributors(ContributorFunc(func(
			context.Context,
			BuildContext,
		) (Contribution, error) {
			return Contribution{Tools: []kit.Tool{contributed}}, nil
		})),
		WithToolMiddleware(func(_ BuildContext, tool kit.Tool) (kit.Tool, error) {
			wrapped = append(wrapped, tool.Definition().Name)

			return tool, nil
		}),
	)

	if _, err := factory.Build(context.Background(), testBuildRequest()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := []string{"base", "contributed"}
	if !slices.Equal(wrapped, want) {
		t.Fatalf("wrapped tools = %v, want %v", wrapped, want)
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

func testBuildRequest() BuildRequest {
	store := entry.NewMemoryStore()
	events := event.NewBus("session-1", store)

	return BuildRequest{
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Memory:      NewMemory("session-1", "execution-1", store, events),
		Entries:     store,
		Events:      events,
	}
}
