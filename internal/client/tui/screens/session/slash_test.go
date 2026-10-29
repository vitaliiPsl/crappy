package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	harnesssession "github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestHelpCommandWorksWhileExecutionIsActive(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, config.Config{}, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	screen.executionID = "running"
	if command := screen.submit("/help"); command != nil {
		t.Fatal("help submitted application work")
	}

	for _, name := range []string{"/help", "/new", "/sessions", "/compact"} {
		if !strings.Contains(screen.statusView(), name) {
			t.Fatalf("help omitted %s: %q", name, screen.statusView())
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
			screen := New(context.Background(), nil, config.Config{}, "session-1")
			defer screen.Close()

			screen.executionID = "running"

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
		screen := New(context.Background(), nil, config.Config{}, "")
		if command := screen.submit(text); command != nil {
			t.Fatalf("invalid command %q submitted application work", text)
		}

		if screen.notice.kind != noticeError || screen.sessionID != "" {
			t.Fatalf("invalid command %q did not produce a local error", text)
		}

		screen.Close()
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
		execution.Func(func(ctx context.Context, params execution.ExecuteParams) (any, error) {
			if string(params.Input) != "null" {
				t.Errorf("compaction received chat input: %s", params.Input)
			}

			select {
			case <-release:
				return compactionexec.Result{Compacted: true}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}),
	))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := assistant.New(h, storage.SessionStore())

	screen := New(ctx, app, config.Config{}, "")
	defer screen.Close()

	missing := screen.submit("/compact")().(executeResultMsg)
	if !errors.Is(missing.err, assistant.ErrSessionRequired) {
		t.Fatalf("compaction without a session returned %v", missing.err)
	}

	if sessions, err := app.Sessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("compaction created an empty session: %+v, %v", sessions, err)
	}

	opened, err := app.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	screen.sessionID = opened.ID()

	accepted := screen.submit("/compact")().(executeResultMsg)
	if accepted.err != nil {
		t.Fatal(accepted.err)
	}

	busy := screen.submit("/compact")().(executeResultMsg)
	if !errors.Is(busy.err, harnesssession.ErrBusy) {
		t.Fatalf("concurrent compaction returned %v, want ErrBusy", busy.err)
	}

	_, wait := screen.Update(accepted)
	if wait == nil {
		t.Fatal("compaction has no completion observer")
	}

	close(release)
	screen.Update(wait())

	result, err := accepted.execution.Wait(ctx)
	if err != nil || result.Outcome != execution.Completed {
		t.Fatalf("compaction result = %+v, error = %v", result, err)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 || entries[0].Type != execution.StartedEntryType || entries[1].Type != execution.FinishedEntryType {
		t.Fatalf("unexpected compaction journal: %+v", entries)
	}

	var started execution.Started
	if err := json.Unmarshal(entries[0].Payload, &started); err != nil {
		t.Fatal(err)
	}

	if started.Executor != compactionexec.ExecutorName {
		t.Fatalf("command used executor %q", started.Executor)
	}
}
