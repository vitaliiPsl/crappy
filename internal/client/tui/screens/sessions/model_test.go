package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestLoadSelectsCurrentSession(t *testing.T) {
	model := testModel(t, "session-2", []state.Record{
		{ID: "session-1"},
		{ID: "session-2"},
	})

	model.Update(model.Init()())

	if model.loading || model.err != nil {
		t.Fatalf("loaded = %t, error = %v", !model.loading, model.err)
	}

	if got := model.sessions[model.selected].ID; got != "session-2" {
		t.Fatalf("selected session = %q, want session-2", got)
	}
}

func TestLoadIncludesUnstoredCurrentSession(t *testing.T) {
	model := testModel(t, "current", []state.Record{{ID: "stored"}})

	model.Update(model.Init()())

	if len(model.sessions) != 2 || model.sessions[0].ID != "current" {
		t.Fatalf("sessions = %#v, want current session first", model.sessions)
	}
}

func TestEnterOpensSelectedSession(t *testing.T) {
	model := testModel(t, "session-1", []state.Record{
		{ID: "session-1"},
		{ID: "session-2"},
	})
	model.Update(model.Init()())
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	action := command().(navigation.Action)

	if action.Operation() != navigation.OperationReset ||
		action.Route().Param(routes.ParamSessionID) != "session-2" {
		t.Fatalf("action = %#v, want reset to session-2", action)
	}
}

func TestNewOpensUncreatedSession(t *testing.T) {
	model := testModel(t, "current", nil)

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'n'}))
	action := command().(navigation.Action)

	if action.Operation() != navigation.OperationReset || action.Route().Param(routes.ParamSessionID) != "" {
		t.Fatalf("action = %#v, want reset to a new session without an ID", action)
	}
}

func TestEscapeReturnsToSession(t *testing.T) {
	model := testModel(t, "current", nil)

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	action := command().(navigation.Action)

	if action.Operation() != navigation.OperationBack {
		t.Fatalf("operation = %v, want back", action.Operation())
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	model := testModel(t, "session-1", []state.Record{
		{ID: "session-1"},
		{ID: "session-2"},
	})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model.Update(model.Init()())
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'd'}))
	if command != nil || model.deleteID != "session-2" {
		t.Fatalf("delete ID = %q, command = %v", model.deleteID, command)
	}

	if !strings.Contains(model.View().Content, "Enter") {
		t.Fatal("view does not show delete confirmation")
	}

	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))

	if model.deleteID != "" {
		t.Fatalf("delete ID = %q after cancellation", model.deleteID)
	}
}

func TestDeleteRemovesSession(t *testing.T) {
	model := testModel(t, "session-1", []state.Record{
		{ID: "session-1"},
		{ID: "session-2"},
	})
	model.Update(model.Init()())
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model.Update(tea.KeyPressMsg(tea.Key{Code: 'd'}))

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model.Update(command())

	if len(model.sessions) != 1 || model.sessions[0].ID != "session-1" {
		t.Fatalf("sessions = %#v, want only session-1", model.sessions)
	}
}

func TestDeleteRejectsCurrentSession(t *testing.T) {
	model := testModel(t, "session-1", []state.Record{{ID: "session-1"}})
	model.Update(model.Init()())

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'd'}))

	if command != nil || !errors.Is(model.err, errCurrentSession) {
		t.Fatalf("command = %v, error = %v", command, model.err)
	}
}

func TestDeleteClosesLiveSessionBeforeRemovingStorage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) (any, error) {
		close(started)
		<-ctx.Done()

		return nil, ctx.Err()
	})
	h := harness.New(storage, harness.WithExecutor("work", executor))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := opened.Execute(ctx, execution.Request{Executor: "work"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("executor did not start")
	}

	model := New(ctx, assistant.New(h, storage.SessionStore()), "other-session")
	defer model.Close()

	deleted := model.deleteSession(opened.ID())().(deletedMsg)
	if deleted.err != nil {
		t.Fatal(deleted.err)
	}

	select {
	case <-subscription.Done():
	default:
		t.Fatal("deleted session retained a live subscription")
	}

	if _, err := h.Session(ctx, opened.ID()); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("deleted session lookup error = %v", err)
	}

	if _, err := opened.Execute(ctx, execution.Request{}); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("execution on deleted session error = %v", err)
	}

	if states, err := storage.SessionStore().List(ctx); err != nil || len(states) != 0 {
		t.Fatalf("sessions after deletion = %+v, %v", states, err)
	}
}

func TestCloseCancelsScreenContext(t *testing.T) {
	model := testModel(t, "current", nil)

	model.Close()

	if model.ctx.Err() == nil {
		t.Fatal("Close() did not cancel the screen context")
	}
}

func TestViewFitsWindow(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 80, height: 20},
		{width: 40, height: 14},
	} {
		model := testModel(t, "2f37556e-c14e-4e01-9e84-103549c27c0b", nil)
		model.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		model.Update(model.Init()())

		view := model.View().Content
		if width, height := lipgloss.Width(view), lipgloss.Height(view); width != size.width || height != size.height {
			t.Fatalf(
				"view = %dx%d, want %dx%d",
				width,
				height,
				size.width,
				size.height,
			)
		}
	}
}

func testModel(t *testing.T, currentID string, sessions []state.Record) *Model {
	t.Helper()

	store := state.NewMemoryStore()
	for _, record := range sessions {
		if err := store.Save(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	h := harness.New(testStorage{store})
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	return New(context.Background(), assistant.New(h, store), currentID)
}

type testStorage struct{ state.Store }

func (s testStorage) SessionStore() state.Store { return s.Store }
func (testStorage) EntryStore() entry.Store     { return entry.NewMemoryStore() }
