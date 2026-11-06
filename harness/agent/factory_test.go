package agent

import (
	"context"
	"errors"
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
			Instructions:    "Be concise.",
			Temperature:     &temperature,
			MaxOutputTokens: &maxOutputTokens,
			Thinking:        &thinking,
		},
		staticModelResolver{model},
		WithContributors(contribute(Contribution{Tools: []kit.Tool{baseTool}})),
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

func TestFactoryReturnsNilAgentWhenConstructionFails(t *testing.T) {
	t.Parallel()

	tool := kittest.NewTool(t, "duplicate", "Duplicate capability")
	factory := New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)},
		WithContributors(contribute(Contribution{Tools: []kit.Tool{tool, tool}})),
	)

	built, err := factory.Build(context.Background(), testBuildRequest())
	if err == nil || built != nil {
		t.Fatalf("failed construction returned agent = %v, error = %v", built, err)
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

func TestFactoryBuildsAgentThatInspectsWithoutRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := kittest.NewModel(t)
	base := kittest.NewTool(t, "base", "Base capability")
	contributed := kittest.NewTool(t, "contributed", "Contributed capability")
	store := entry.NewMemoryStore()
	build := testBuildRequestOn(store)

	prior := kit.NewModelMessage(kit.NewToolCallContent(kit.NewToolCall("call-1", "base", nil)))
	if err := build.History.Append(ctx, prior); err != nil {
		t.Fatal(err)
	}

	before, err := store.List(ctx, build.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}

	contributions, wraps := 0, 0
	factory := New(kit.AgentConfig{Instructions: "base instructions"}, staticModelResolver{model},
		WithToolMiddleware(func(_ BuildContext, tool kit.Tool) (kit.Tool, error) {
			wraps++

			return tool, nil
		}),
		WithContributors(
			contribute(Contribution{Tools: []kit.Tool{base}}),
			ContributorFunc(func(_ context.Context, received BuildContext) (Contribution, error) {
				contributions++

				if !reflect.DeepEqual(received.Request, build) || received.Model != model {
					t.Fatalf("contributor received %+v, want the supplied request and model", received)
				}

				return Contribution{
					Source:       "workspace",
					Instructions: []string{"workspace guidance"},
					Tools:        []kit.Tool{contributed},
					Options: []adkagent.Option{
						adkagent.WithOnModelRequestReady(func(*kit.RunContext, kit.ModelRequest) (bool, error) {
							t.Fatal("inspection ran a readiness hook")

							return false, nil
						}),
					},
				}, nil
			}),
		),
	)

	loaded := inspect(t, factory, build)

	if loaded.InstructionsText() != "base instructions\n\n# workspace\n\nworkspace guidance" ||
		loaded.Instructions[0].Source != "" || loaded.Instructions[1].Source != "workspace" ||
		!reflect.DeepEqual(loaded.Tools, []kit.Tool{base, contributed}) ||
		!reflect.DeepEqual(loaded.Messages, []kit.Message{prior}) {
		t.Fatalf("inspected context = %+v", loaded)
	}

	after, err := store.List(ctx, build.SessionID, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("inspection changed the session log: %+v, %v", after, err)
	}

	if contributions != 1 || wraps != 2 {
		t.Fatalf("contributions=%d, wraps=%d, want 1/2", contributions, wraps)
	}

	model.AssertNeverCalled(t)
	base.AssertNeverCalled(t)
	contributed.AssertNeverCalled(t)
}

func TestFactoryPutsConfiguredInstructionsBeforeContributionsInOrder(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	factory := New(kit.AgentConfig{Instructions: "persona"}, staticModelResolver{model},
		WithContributors(
			contribute(Contribution{Source: "policy", Instructions: []string{"base"}}),
			contribute(Contribution{Source: "first contributor", Instructions: []string{" first ", ""}}),
			contribute(Contribution{Source: "last contributor", Instructions: []string{"second"}}),
		),
	)

	loaded := inspect(t, factory, testBuildRequest())

	if loaded.InstructionsText() != "persona\n\n# policy\n\nbase\n\n# first contributor\n\nfirst\n\n# last contributor\n\nsecond" {
		t.Fatalf("instruction order = %+v", loaded.Instructions)
	}

	var sources []string
	for _, section := range loaded.Instructions {
		sources = append(sources, section.Source)
	}

	if !slices.Equal(sources, []string{"", "policy", "first contributor", "last contributor"}) {
		t.Fatalf("instruction sources = %v", sources)
	}

	model.AssertNeverCalled(t)
}

func TestFactoryBuildAppliesContributions(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	store := entry.NewMemoryStore()
	buildRequest := testBuildRequestOn(store)
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

		if build.Request.History != buildRequest.History {
			t.Fatal("BuildContext does not contain the supplied history")
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
			Options:      []adkagent.Option{adkagent.WithTemperature(0.5)},
		}, nil
	})
	factory := New(
		kit.AgentConfig{},
		staticModelResolver{model},
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
	if got, want := request.Instructions, "# contributor\n\nUse tools when useful."; got != want {
		t.Fatalf("Instructions = %q, want %q", got, want)
	}

	if got, want := request.Tools, []kit.Tool{contributedTool}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Tools = %v, want %v", got, want)
	}

	if request.Config.Temperature == nil || *request.Config.Temperature != 0.5 {
		t.Fatalf("Temperature = %v, want 0.5", request.Config.Temperature)
	}

	recorded, err := store.List(context.Background(), buildRequest.SessionID, 0)
	if err != nil || len(recorded) != 2 {
		t.Fatalf("recorded entries = %+v, error = %v, want input and response", recorded, err)
	}

	for _, value := range recorded {
		if value.Type != MessageEntryType || value.ExecutionID != buildRequest.ExecutionID {
			t.Fatalf("agent did not append to the session history: %+v", value)
		}
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

	if _, err := factory.Build(context.Background(), testBuildRequest()); !errors.Is(err, wantErr) {
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
		WithContributors(
			contribute(Contribution{Tools: []kit.Tool{base}}),
			contribute(Contribution{Tools: []kit.Tool{contributed}}),
		),
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

func TestFactoryUsesPreparedTools(t *testing.T) {
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
	selected := kittest.NewTool(t, "selected", "Selected tool")
	prepared := kittest.NewTool(t, "selected", "Prepared tool", kittest.ToolResult{Result: "prepared output"})
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithContributors(contribute(Contribution{Tools: []kit.Tool{selected}})),
		WithToolMiddleware(func(build BuildContext, tool kit.Tool) (kit.Tool, error) {
			if build.Request.ExecutionID != "execution-1" || tool != selected {
				t.Fatalf("unexpected preparation: execution=%q, tool=%v", build.Request.ExecutionID, tool)
			}

			return prepared, nil
		}),
	)

	preview := inspect(t, factory, testBuildRequest())

	if len(preview.Tools) != 1 || preview.Tools[0] != prepared {
		t.Fatalf("preview tools = %v, want prepared tool", preview.Tools)
	}

	built, err := factory.Build(context.Background(), testBuildRequest())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("use selected"))); err != nil {
		t.Fatal(err)
	}

	if model.CallAt(0).Tools[0] != prepared {
		t.Fatal("model request did not use the prepared tool")
	}

	prepared.AssertCallCount(t, 1)
	selected.AssertNeverCalled(t)
}

func TestFactoryToolMiddlewareFailureStopsBuild(t *testing.T) {
	t.Parallel()

	failure := errors.New("cannot prepare tool")
	model := kittest.NewModel(t)
	factory := New(kit.AgentConfig{}, staticModelResolver{model},
		WithContributors(contribute(Contribution{Tools: []kit.Tool{kittest.NewTool(t, "lookup", "")}})),
		WithToolMiddleware(func(BuildContext, kit.Tool) (kit.Tool, error) {
			return nil, failure
		}),
	)

	if _, err := factory.Build(context.Background(), testBuildRequest()); !errors.Is(err, failure) {
		t.Fatalf("Build() error = %v, want preparation failure", err)
	}

	model.AssertNeverCalled(t)
}

func contribute(contribution Contribution) Contributor {
	return ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
		return contribution, nil
	})
}

func inspect(t *testing.T, factory *Factory, request BuildRequest) kit.AgentContext {
	t.Helper()

	built, err := factory.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	inspected, err := built.Context(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return inspected
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

func testBuildRequest() BuildRequest {
	return testBuildRequestOn(entry.NewMemoryStore())
}

func testBuildRequestOn(store entry.Store) BuildRequest {
	return BuildRequest{
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		History:     NewHistory("session-1", "execution-1", store, event.NewBroker("session-1", store)),
	}
}
