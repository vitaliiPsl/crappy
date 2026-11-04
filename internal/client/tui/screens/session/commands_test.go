package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestLoadModelReadsAssistantSelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := assistant.New(config.Config{
		ProviderID: "provider",
		ModelID:    "selected-model",
		Generation: config.Generation{Thinking: "high"},
	}, h, storage, nil, token.NewCharacterEstimator())

	screen := New(ctx, app, "")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	commands := screen.Init()().(tea.BatchMsg)
	for _, command := range commands {
		if message, ok := command().(modelMsg); ok {
			screen.Update(message)
		}
	}

	if header := screen.headerView(); !strings.Contains(header, "provider / selected-model · high") {
		t.Fatalf("header omitted assistant model selection: %q", header)
	}

	if records, err := app.ListSessions(ctx); err != nil || len(records) != 0 {
		t.Fatalf("initializing the screen created sessions: %+v, %v", records, err)
	}
}

func TestLoadWorkdirReadsSessionState(t *testing.T) {
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

	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	screen.Update(screen.loadWorkdir()())

	if screen.workdir != directory {
		t.Fatalf("draft workdir = %q, want %q", screen.workdir, directory)
	}

	if records, err := app.ListSessions(ctx); err != nil || len(records) != 0 {
		t.Fatalf("draft lookup created a session: %+v, %v", records, err)
	}

	directory = t.TempDir()

	created, err := app.CreateSession(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}

	screen.sessionID = created.ID
	screen.Update(screen.loadWorkdir()())

	if screen.workdir != directory {
		t.Fatalf("session workdir = %q, want %q", screen.workdir, directory)
	}

	opened, err := h.Session(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	directory = t.TempDir()
	if err := opened.State().Set(ctx, workdir.StateKey, directory); err != nil {
		t.Fatal(err)
	}

	screen.Update(screen.loadWorkdir()())

	if screen.workdir != directory {
		t.Fatalf("updated workdir = %q, want %q", screen.workdir, directory)
	}
}

func TestExecuteReportsTerminalPersistenceFailure(t *testing.T) {
	for _, streamClosesFirst := range []bool{true, false} {
		name := "wait first"
		if streamClosesFirst {
			name = "stream closes first"
		}

		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			storage, err := jsonl.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			failure := errors.New("storage unavailable")
			store := executionTestStore{
				sessions: storage.SessionStore(),
				entries:  finishFailureStore{Store: storage.EntryStore(), err: failure},
			}
			h := harness.New(store, harness.WithExecutor(agent.ExecutorName,
				execution.Func(func(context.Context, execution.ExecuteParams) error {
					return nil
				}),
			))
			t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

			opened, err := h.Session(ctx, "")
			if err != nil {
				t.Fatal(err)
			}

			screen := New(ctx, assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator()), opened.ID())
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

			accepted := screen.execute(kit.NewUserMessage(kit.NewTextContent("hello")))().(executeResultMsg)
			if accepted.err != nil {
				t.Fatal(accepted.err)
			}

			started := executionEvent(t, execution.StartedEntryType, accepted.executionID, execution.Started{})
			screen.applyEvent(started)

			_, wait := screen.Update(accepted)
			if wait == nil {
				t.Fatal("accepted execution has no completion observer")
			}

			closed := eventMsg{target: screen, open: false, err: event.ErrClosed}
			if streamClosesFirst {
				screen.Update(closed)
			}

			screen.Update(wait())
			screen.applyEvent(started)

			if !streamClosesFirst {
				screen.Update(closed)
			}

			if !errors.Is(screen.terminalErr, failure) {
				t.Fatalf("terminal error = %v, want %v", screen.terminalErr, failure)
			}

			if _, err := screen.assistant.GetExecutionStatus(ctx, opened.ID(), accepted.executionID); !errors.Is(err, failure) {
				t.Fatalf("lookup lost terminal persistence failure: %v", err)
			}

			if screen.running() {
				t.Fatal("failed execution still appears active")
			}

			if status := screen.statusView(); !strings.Contains(status, failure.Error()) || strings.Contains(status, workingLabel) {
				t.Fatalf("status = %q, want persistence failure", status)
			}

			if screen.submit("retry") != nil {
				t.Fatal("closed session accepted another submission")
			}
		})
	}
}

func TestWaitForExecutionStopsWhenScreenCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName,
		execution.Func(func(ctx context.Context, _ execution.ExecuteParams) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	defer close(release)

	opened, err := h.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	screen := New(ctx, assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator()), opened.ID())
	defer screen.Close()

	accepted := screen.execute(kit.NewUserMessage(kit.NewTextContent("hello")))().(executeResultMsg)
	if accepted.err != nil {
		t.Fatal(accepted.err)
	}

	_, wait := screen.Update(accepted)
	screen.Close()
	screen.Update(wait())

	if screen.terminalErr != nil {
		t.Fatalf("detaching reported an execution failure: %v", screen.terminalErr)
	}

	status, err := screen.assistant.GetExecutionStatus(ctx, opened.ID(), accepted.executionID)
	if err != nil || status != execution.Running {
		t.Fatalf("detaching changed execution status: %q, %v", status, err)
	}
}

func TestFirstSubmitCreatesSession(t *testing.T) {
	storage, err := jsonl.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	screen := New(context.Background(), assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator()), "")
	defer screen.Close()

	created := screen.submit("fix the compaction")().(createdMsg)
	if created.err != nil {
		t.Fatal(created.err)
	}

	screen.handleCreated(created)

	if screen.sessionID == "" || screen.sessionID != created.sessionID {
		t.Fatalf("sessionID = %q, want created session %q", screen.sessionID, created.sessionID)
	}

	sessions, err := storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 || sessions[0].ID != screen.sessionID {
		t.Fatalf("List() = %#v, want created session", sessions)
	}
}

type executionTestStore struct {
	sessions state.Store
	entries  entry.Store
}

func (s executionTestStore) SessionStore() state.Store {
	return s.sessions
}

func (s executionTestStore) EntryStore() entry.Store {
	return s.entries
}

type finishFailureStore struct {
	entry.Store
	err error
}

func (s finishFailureStore) Append(ctx context.Context, sessionID string, entries ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range entries {
		if value.Type == execution.FinishedEntryType {
			return nil, s.err
		}
	}

	return s.Store.Append(ctx, sessionID, entries...)
}
