package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
			Temperature:     &temperature,
			MaxOutputTokens: &maxOutputTokens,
			Thinking:        &thinking,
		},
		staticModelResolver{model},
		WithInstructions("Be concise."),
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

func TestFactoryBuildExposesCurrentContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := kittest.NewModel(t)
	base := kittest.NewTool(t, "base", "Base capability")
	contributed := kittest.NewTool(t, "contributed", "Contributed capability")
	build := testBuildRequest()

	prior := kit.NewUserMessage(kit.NewTextContent("prior work"))
	if err := build.Memory.Record(ctx, prior); err != nil {
		t.Fatal(err)
	}

	contributions, wraps := 0, 0
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithInstructions("base instructions"),
		WithTools(base),
		WithToolMiddleware(func(_ BuildContext, tool kit.Tool) (kit.Tool, error) {
			wraps++

			return tool, nil
		}),
		WithContributors(ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
			contributions++

			return Contribution{
				Source:       "workspace",
				Instructions: []string{"workspace guidance"},
				Tools:        []kit.Tool{contributed},
				Options: []adkagent.Option{
					adkagent.WithOnContext(func(_ context.Context, current kit.AgentContext) (kit.AgentContext, error) {
						current.Instructions = append(current.Instructions, kit.Instruction{
							Source: "task",
							Text:   fmt.Sprintf("%d conversation messages", len(current.Messages)),
						})

						return current, nil
					}),
					adkagent.WithOnModelRequestReady(func(*kit.RunContext, kit.ModelRequest) (bool, error) {
						t.Fatal("inspection ran a readiness hook")

						return false, nil
					}),
				},
			}, nil
		})),
	)

	built, err := factory.Build(ctx, build)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := built.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if resolved.InstructionsText() != "base instructions\n\n# workspace\n\nworkspace guidance\n\n# task\n\n1 conversation messages" ||
		resolved.Instructions[0].Source != "" || resolved.Instructions[1].Source != "workspace" ||
		!reflect.DeepEqual(resolved.Tools, []kit.Tool{base, contributed}) ||
		!reflect.DeepEqual(resolved.Messages, []kit.Message{prior}) {
		t.Fatalf("resolved context = %+v", resolved)
	}

	if err := build.Memory.Record(ctx, kit.NewModelMessage(kit.NewTextContent("new response"))); err != nil {
		t.Fatal(err)
	}

	updated, err := built.Context(ctx)
	if err != nil || len(updated.Messages) != 2 || updated.Instructions[2].Text != "2 conversation messages" {
		t.Fatalf("updated context = %+v, error=%v", updated, err)
	}

	if contributions != 1 || wraps != 2 {
		t.Fatalf("inspection repeated execution preparation: contributions=%d, wraps=%d", contributions, wraps)
	}

	history, err := build.Memory.History(ctx)
	if err != nil || len(history) != 2 {
		t.Fatalf("inspection wrote conversation history: %+v, error=%v", history, err)
	}

	model.AssertCallCount(t, 0)
	base.AssertNeverCalled(t)
	contributed.AssertNeverCalled(t)
}

func TestFactoryPreservesInstructionContributionOrder(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	dynamic := "second"
	contributions := 0
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithInstructionsSource("policy", "base"),
		WithContributors(
			ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
				contributions++

				return Contribution{
					Source:       "first contributor",
					Instructions: []string{" first "},
					Options: []adkagent.Option{
						adkagent.WithOnContext(func(_ context.Context, current kit.AgentContext) (kit.AgentContext, error) {
							if current.InstructionsText() != "# policy\n\nbase\n\n# first contributor\n\nfirst" {
								t.Fatalf("later instructions were resolved early: %+v", current.Instructions)
							}

							current.Instructions = append(current.Instructions, kit.Instruction{Source: "task", Text: dynamic})

							return current, nil
						}),
					},
				}, nil
			}),
			ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
				contributions++

				return Contribution{Source: "last contributor", Instructions: []string{"third"}}, nil
			}),
		),
	)

	built, err := factory.Build(context.Background(), testBuildRequest())
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"second", "updated second"} {
		dynamic = text

		current, err := built.Context(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if current.InstructionsText() != "# policy\n\nbase\n\n# first contributor\n\nfirst\n\n# task\n\n"+text+"\n\n# last contributor\n\nthird" {
			t.Fatalf("instruction order = %+v", current.Instructions)
		}

		var sources []string
		for _, section := range current.Instructions {
			sources = append(sources, section.Source)
		}

		if !slices.Equal(sources, []string{"policy", "first contributor", "task", "last contributor"}) {
			t.Fatalf("instruction sources = %v", sources)
		}
	}

	if contributions != 2 {
		t.Fatalf("contributors ran %d times, want once each", contributions)
	}

	model.AssertCallCount(t, 0)
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
			Source:       "contributor",
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
		WithInstructions("Be concise."),
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
	if got, want := request.Instructions, "Be concise.\n\n# contributor\n\nUse tools when useful."; got != want {
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

func TestFactoryContextHooksInspectPreparedTools(t *testing.T) {
	t.Parallel()

	call := kit.NewToolCall("call-1", "selected", nil)
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewToolCallContent(call)), FinishReason: kit.FinishReasonToolCall,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
		}},
	)
	base := kittest.NewTool(t, "base", "Base tool")
	selected := kittest.NewTool(t, "selected", "Selected tool")
	prepared := kittest.NewTool(t, "selected", "Prepared tool", kittest.ToolResult{Result: "prepared output"})
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithTools(selected),
		WithToolMiddleware(func(build BuildContext, tool kit.Tool) (kit.Tool, error) {
			if build.Request.ExecutionID != "execution-1" || tool != selected {
				t.Fatalf("unexpected preparation: execution=%q, tool=%v", build.Request.ExecutionID, tool)
			}

			return prepared, nil
		}),
		WithAgentOptions(adkagent.WithOnContext(func(_ context.Context, current kit.AgentContext) (kit.AgentContext, error) {
			if len(current.Tools) != 1 || current.Tools[0] != prepared {
				t.Fatalf("context hook tools = %v, want prepared tool", current.Tools)
			}

			current.Tools[0] = base

			return current, nil
		})),
	)

	built, err := factory.Build(context.Background(), testBuildRequest())
	if err != nil {
		t.Fatal(err)
	}

	preview, err := built.Context(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Tools) != 1 || preview.Tools[0] != prepared {
		t.Fatalf("preview tools = %v, want prepared tool", preview.Tools)
	}

	if _, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("use selected"))); err != nil {
		t.Fatal(err)
	}

	if model.CallAt(0).Tools[0] != prepared {
		t.Fatal("model request did not use the prepared tool")
	}

	prepared.AssertCallCount(t, 1)
	selected.AssertNeverCalled(t)
	base.AssertNeverCalled(t)
}

func TestFactoryToolMiddlewareFailureStopsBuild(t *testing.T) {
	t.Parallel()

	failure := errors.New("cannot prepare tool")
	model := kittest.NewModel(t)
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithTools(kittest.NewTool(t, "lookup", "")),
		WithToolMiddleware(func(BuildContext, kit.Tool) (kit.Tool, error) {
			return nil, failure
		}),
	)

	if _, err := factory.Build(context.Background(), testBuildRequest()); !errors.Is(err, failure) {
		t.Fatalf("Build() error = %v, want preparation failure", err)
	}

	model.AssertNeverCalled(t)
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
