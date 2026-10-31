package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

func TestContextResolverLeavesUnfinishedToolCallsUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	bus := event.NewBus("session-1", store)
	defer bus.Close()

	prior := kit.NewModelMessage(kit.NewToolCallContent(kit.NewToolCall("call-1", "write", nil)))
	if err := bus.Commit(ctx, "previous", MessageEntryType, prior); err != nil {
		t.Fatal(err)
	}

	before, err := store.List(ctx, "session-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)
	tool := kittest.NewTool(t, "write", "Write a file")
	configured := NewContextResolver(New(kit.AgentConfig{}, staticModelResolver{model},
		WithTools(tool),
		WithContributors(ContributorFunc(func(_ context.Context, build BuildContext) (Contribution, error) {
			if build.Request.SessionID != "session-1" || build.Request.ExecutionID != "" ||
				build.Request.Entries != store || build.Request.Events != bus {
				t.Fatalf("context used different session resources: %+v", build.Request)
			}

			return Contribution{
				Source: "Session", Instructions: []string{"Continue carefully."},
				Options: []adkagent.Option{adkagent.WithOnModelRequestReady(func(*kit.RunContext, kit.ModelRequest) (bool, error) {
					t.Fatal("context ran execution hooks")

					return false, nil
				})},
			}, nil
		})),
	))

	resolved, err := configured.Resolve(ctx, "session-1", nil, store, bus)
	if err != nil {
		t.Fatal(err)
	}

	if resolved.InstructionsText() != "# Session\n\nContinue carefully." ||
		!reflect.DeepEqual(resolved.Messages, []kit.Message{prior}) || len(resolved.Tools) != 1 || resolved.Tools[0] != tool {
		t.Fatalf("resolved context = %+v", resolved)
	}

	after, err := store.List(ctx, "session-1", 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("context changed unfinished history: %+v, %v", after, err)
	}

	model.AssertNeverCalled(t)
	tool.AssertNeverCalled(t)
}

func TestContextResolverPropagatesBuildFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("instructions unavailable")
	configured := NewContextResolver(New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)},
		WithContributors(ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
			return Contribution{}, failure
		})),
	))

	if _, err := configured.Resolve(context.Background(), "session-1", nil, nil, nil); !errors.Is(err, failure) {
		t.Fatalf("Resolve() error = %v, want build failure", err)
	}
}
