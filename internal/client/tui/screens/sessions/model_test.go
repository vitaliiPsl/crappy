package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

func TestLoadSelectsCurrentSession(t *testing.T) {
	model := testModel("session-2", []session.State{
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
	model := testModel("current", []session.State{{ID: "stored"}})

	model.Update(model.Init()())

	if len(model.sessions) != 2 || model.sessions[0].ID != "current" {
		t.Fatalf("sessions = %#v, want current session first", model.sessions)
	}
}

func TestEnterOpensSelectedSession(t *testing.T) {
	model := testModel("session-1", []session.State{
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
	model := testModel("current", nil)

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'n'}))
	action := command().(navigation.Action)

	if action.Operation() != navigation.OperationReset || action.Route().Param(routes.ParamSessionID) != "" {
		t.Fatalf("action = %#v, want reset to a new session without an ID", action)
	}
}

func TestEscapeReturnsToSession(t *testing.T) {
	model := testModel("current", nil)

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	action := command().(navigation.Action)

	if action.Operation() != navigation.OperationBack {
		t.Fatalf("operation = %v, want back", action.Operation())
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	model := testModel("session-1", []session.State{
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
	model := testModel("session-1", []session.State{
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
	model := testModel("session-1", []session.State{{ID: "session-1"}})
	model.Update(model.Init()())

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'd'}))

	if command != nil || !errors.Is(model.err, errCurrentSession) {
		t.Fatalf("command = %v, error = %v", command, model.err)
	}
}

func TestCloseCancelsScreenContext(t *testing.T) {
	model := testModel("current", nil)

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
		model := testModel("2f37556e-c14e-4e01-9e84-103549c27c0b", nil)
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

func testModel(currentID string, sessions []session.State) *Model {
	appRuntime := runtime.New(
		context.Background(),
		staticSessionStore{sessions: sessions},
		nil,
		nil,
		nil,
	)

	return New(context.Background(), appRuntime, currentID)
}

type staticSessionStore struct {
	sessions []session.State
}

func (s staticSessionStore) List(context.Context) ([]session.State, error) {
	return s.sessions, nil
}

func (staticSessionStore) Get(context.Context, string) (session.State, error) {
	return session.State{}, session.ErrNotFound
}

func (staticSessionStore) Save(context.Context, session.State) error {
	return nil
}

func (staticSessionStore) Delete(context.Context, string) error {
	return nil
}
