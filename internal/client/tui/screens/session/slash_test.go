package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestHelpCommandWorksWhileExecutionIsActive(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	screen.applyExecution(execution.Record{ID: "running", Status: execution.Running})

	if command := screen.submit("/help"); command != nil {
		t.Fatal("help submitted application work")
	}

	for _, name := range []string{"/help", "/new", "/sessions", "/compact", "/context", "/queue", "/resume"} {
		if !strings.Contains(screen.View().Content, name) {
			t.Fatalf("help omitted %s: %q", name, screen.View().Content)
		}
	}

	if !screen.running() {
		t.Fatal("help changed the execution state")
	}
}

func TestNavigationCommandsWorkWhileExecutionIsActive(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		command   string
		operation navigation.Operation
		route     navigation.RouteName
		sessionID string
	}{
		{command: "/new", operation: navigation.OperationReset, route: routes.SessionName},
		{command: "/sessions", operation: navigation.OperationToggle, route: routes.SessionsName, sessionID: "session-1"},
	} {
		t.Run(test.command, func(t *testing.T) {
			screen := New(context.Background(), nil, "session-1")
			defer screen.Close()

			screen.applyExecution(execution.Record{ID: "running", Status: execution.Running})

			command := screen.submit(test.command)
			if command == nil {
				t.Fatal("navigation command was ignored while busy")
			}

			action := command().(navigation.Action)
			if action.Operation() != test.operation || action.Route().Name() != test.route || action.Route().Param(routes.ParamSessionID) != test.sessionID {
				t.Fatalf("unexpected navigation action: %+v", action)
			}

			if !screen.running() {
				t.Fatal("navigation command changed the execution state")
			}
		})
	}
}

func TestInvalidCommandsStayOutOfConversation(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"/unknown", "/compact extra", "/"} {
		screen := New(context.Background(), nil, "")
		if command := screen.submit(text); command != nil {
			t.Fatalf("invalid command %q submitted application work", text)
		}

		if screen.notice.kind != noticeError || screen.sessionID != "" {
			t.Fatalf("invalid command %q did not produce a local error", text)
		}

		screen.Close()
	}
}

func TestContextCommandShowsLocalSnapshotsWhileExecutionIsActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)
	tool := kittest.NewTool(t, "bash", "Run a shell command")
	agents := agent.New(kit.AgentConfig{Instructions: "Be concise."}, contextModelResolver{model},
		agent.WithContributors(agent.ContributorFunc(func(context.Context, agent.BuildContext) (agent.Contribution, error) {
			return agent.Contribution{Tools: []kit.Tool{tool}}, nil
		})),
	)
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)))
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := assistant.New(config.Config{}, h, storage, agents, token.NewCharacterEstimator())

	created, err := app.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	opened, err := h.Session(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("first message"))); err != nil {
		t.Fatal(err)
	}

	screen := New(ctx, app, opened.ID())
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	screen.applyExecution(execution.Record{ID: "running", Status: execution.Running})

	load := screen.submit("/context")
	if load == nil {
		t.Fatal("context command was ignored while busy")
	}

	if repeated := screen.submit("/context"); repeated != nil {
		t.Fatal("context command started concurrent requests")
	}

	if _, command := screen.Update(load()); command != nil {
		t.Fatal("context output started navigation or execution")
	}

	view := screen.View().Content
	for _, want := range []string{"/context", "tokens", "Instructions", "Core", "Conversation", "1 message", "Tools", "1 tool", "bash"} {
		if !strings.Contains(view, want) {
			t.Fatalf("context output omitted %q: %s", want, view)
		}
	}

	if !screen.running() || !strings.Contains(screen.statusView(), workingLabel) {
		t.Fatal("context command changed the execution state")
	}

	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("second message"))); err != nil {
		t.Fatal(err)
	}

	screen.Update(screen.submit("/context")())

	if view := screen.View().Content; strings.Count(view, "/context") != 2 || !strings.Contains(view, "2 messages") {
		t.Fatalf("context command did not append a fresh snapshot: %s", view)
	}

	if command := screen.submit("/help"); command != nil {
		t.Fatal("help submitted application work")
	}

	resolved, err := app.Context(ctx, opened.ID())
	if err != nil || len(resolved.Messages) != 2 {
		t.Fatalf("command output entered agent context: %+v, %v", resolved.Messages, err)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil || len(entries) != 2 || entries[0].Type != agent.MessageEntryType || entries[1].Type != agent.MessageEntryType {
		t.Fatalf("command output changed persisted history: %+v, %v", entries, err)
	}

	model.AssertNeverCalled(t)
	tool.AssertNeverCalled(t)
}

func TestContextCommandRequiresExistingSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	screen := New(ctx, app, "")
	defer screen.Close()

	result := screen.submit("/context")().(contextMsg)
	if !errors.Is(result.err, harness.ErrSessionRequired) {
		t.Fatalf("context without a session returned %v", result.err)
	}

	screen.Update(result)

	if screen.contextLoading || screen.notice.kind != noticeError || screen.sessionID != "" {
		t.Fatal("context failure did not remain local to the current screen")
	}

	if sessions, err := app.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("context command created a session: %+v, %v", sessions, err)
	}
}

func TestCompactCommandUsesNormalExecutionLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	h := harness.New(storage, harness.WithExecutor(compactionexec.ExecutorName,
		execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
			if string(params.Input) != "null" {
				t.Errorf("compaction received chat input: %s", params.Input)
			}

			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	screen := New(ctx, app, "")
	defer screen.Close()

	missing := screen.submit("/compact")().(executeResultMsg)
	if !errors.Is(missing.err, harness.ErrSessionRequired) {
		t.Fatalf("compaction without a session returned %v", missing.err)
	}

	if sessions, err := app.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("compaction created an empty session: %+v, %v", sessions, err)
	}

	created, err := app.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	opened, err := h.Session(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	screen.sessionID = opened.ID()

	accepted := screen.submit("/compact")().(executeResultMsg)
	if accepted.err != nil {
		t.Fatal(accepted.err)
	}

	screen.applyEvent(executionEvent(t, execution.EntryType, accepted.executionID, execution.Record{Status: execution.Running}))

	queued := screen.submit("/compact")().(executeResultMsg)
	if queued.err != nil {
		t.Fatal(queued.err)
	}

	if status, err := app.GetExecutionStatus(ctx, created.ID, queued.executionID); err != nil || status != execution.Queued {
		t.Fatalf("compaction was not queued: %s, %v", status, err)
	}

	_, wait := screen.Update(accepted)
	if wait == nil {
		t.Fatal("compaction has no completion observer")
	}

	close(release)
	screen.Update(wait())

	result, err := app.WaitExecution(ctx, created.ID, accepted.executionID)
	if err != nil || result != execution.Completed {
		t.Fatalf("compaction result = %+v, error = %v", result, err)
	}

	if status, err := app.WaitExecution(ctx, created.ID, queued.executionID); err != nil || status != execution.Completed {
		t.Fatalf("queued compaction = %s, %v", status, err)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 6 {
		t.Fatalf("unexpected compaction journal: %+v", entries)
	}

	for i, status := range []execution.Status{execution.Queued, execution.Running, execution.Queued, execution.Completed, execution.Running, execution.Completed} {
		if entries[i].Type != execution.EntryType {
			t.Fatalf("unexpected compaction entry: %+v", entries[i])
		}

		var record execution.Record
		if err := json.Unmarshal(entries[i].Payload, &record); err != nil {
			t.Fatal(err)
		}

		if record.Executor != compactionexec.ExecutorName || record.Status != status {
			t.Fatalf("compaction entry = %+v, want %s from %s", record, status, compactionexec.ExecutorName)
		}
	}
}

type contextModelResolver struct {
	model kit.Model
}

func (r contextModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
