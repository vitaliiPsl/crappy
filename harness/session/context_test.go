package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestContextUsesSessionResourcesWithoutExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	values := newSessionState(t, "session-1")
	if err := values.Set(ctx, "guidance", "Follow this session's guidance."); err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)
	tool := kittest.NewTool(t, "lookup", "Lookup information")

	var opened *Session

	factory := agent.New(kit.AgentConfig{}, staticModelResolver{model},
		agent.WithTools(tool),
		agent.WithContributors(agent.ContributorFunc(func(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
			if build.Request.SessionID != opened.ID() || build.Request.ExecutionID != "" ||
				build.Request.State != values || build.Request.Entries != store || build.Request.Events != opened.events {
				t.Fatalf("context did not use session resources: %+v", build.Request)
			}

			var guidance string
			if _, err := build.Request.State.Get(ctx, "guidance", &guidance); err != nil {
				return agent.Contribution{}, err
			}

			return agent.Contribution{Source: "Session", Instructions: []string{guidance}}, nil
		})),
	)

	opened, err := New(ctx, "session-1", values, store, execution.NewRegistry(), agent.NewContextResolver(factory))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(opened.Close)

	prior := kit.NewUserMessage(kit.NewTextContent("prior work"))
	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, prior); err != nil {
		t.Fatal(err)
	}

	before, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := opened.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if resolved.InstructionsText() != "# Session\n\nFollow this session's guidance." ||
		!reflect.DeepEqual(resolved.Messages, []kit.Message{prior}) || len(resolved.Tools) != 1 || resolved.Tools[0] != tool {
		t.Fatalf("resolved context = %+v", resolved)
	}

	after, err := opened.Entries(ctx, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("context resolution changed the journal: %v, %v", after, err)
	}

	if err := values.Set(ctx, "guidance", "Updated guidance."); err != nil {
		t.Fatal(err)
	}

	updated, err := opened.Context(ctx)
	if err != nil || updated.InstructionsText() != "# Session\n\nUpdated guidance." {
		t.Fatalf("session state was not resolved again: %+v, %v", updated, err)
	}

	model.AssertNeverCalled(t)
	tool.AssertNeverCalled(t)
}

func TestContextRespectsCancellationAndClosure(t *testing.T) {
	t.Parallel()

	ctx, cancelSession := context.WithCancel(context.Background())
	defer cancelSession()

	opened := newSession(ctx, t, "session-1", entry.NewMemoryStore(), execution.NewRegistry())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := opened.Context(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Context() error = %v, want caller cancellation", err)
	}

	cancelSession()

	if _, err := opened.Context(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Context() error = %v, want closed lifetime", err)
	}

	closed := newSession(context.Background(), t, "session-2", entry.NewMemoryStore(), execution.NewRegistry())
	closed.Close()

	if _, err := closed.Context(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Context() error = %v, want closed session", err)
	}
}

func TestContextPreparationDoesNotBlockExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
	}})
	factory := agent.New(kit.AgentConfig{}, staticModelResolver{model},
		agent.WithContributors(agent.ContributorFunc(func(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
			if build.Request.ExecutionID != "" {
				return agent.Contribution{}, nil
			}

			close(started)
			<-ctx.Done()

			return agent.Contribution{}, ctx.Err()
		})),
	)
	opened := newAgentSession(ctx, t, "session-1", entry.NewMemoryStore(), factory)

	queryCtx, cancelQuery := context.WithCancel(ctx)
	defer cancelQuery()

	done := make(chan error, 1)
	go func() {
		_, err := opened.Context(queryCtx)
		done <- err
	}()

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("context preparation did not start")
	}

	current, err := opened.Execute(ctx, execution.Request{Input: kit.NewUserMessage(kit.NewTextContent("work"))})
	if err != nil {
		t.Fatal(err)
	}

	result, err := current.Wait(ctx)
	if err != nil || result.Outcome != execution.Completed {
		t.Fatalf("execution was blocked by context preparation: %+v, %v", result, err)
	}

	select {
	case err := <-done:
		t.Fatalf("context preparation ended before cancellation: %v", err)
	default:
	}

	cancelQuery()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Context() error = %v, want caller cancellation", err)
		}
	case <-ctx.Done():
		t.Fatal("context preparation did not honor cancellation")
	}
}

func TestContextCanReadDuringExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	model := &blockingModel{started: make(chan struct{}), release: make(chan struct{})}
	opened := newAgentSession(ctx, t, "session-1", entry.NewMemoryStore(), agent.New(kit.AgentConfig{}, staticModelResolver{model}))
	input := kit.NewUserMessage(kit.NewTextContent("work in progress"))

	current, err := opened.Execute(ctx, execution.Request{Input: input})
	if err != nil {
		t.Fatal(err)
	}

	<-model.started

	before, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := opened.Context(ctx)
	if err != nil || !reflect.DeepEqual(resolved.Messages, []kit.Message{input}) {
		t.Fatalf("active session context = %+v, %v", resolved, err)
	}

	after, err := opened.Entries(ctx, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("context resolution changed the active execution journal: %v, %v", after, err)
	}

	select {
	case <-current.Done():
		t.Fatal("context resolution interrupted the active execution")
	default:
	}

	close(model.release)

	if _, err := current.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
