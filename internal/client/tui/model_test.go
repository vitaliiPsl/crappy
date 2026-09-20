package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

func TestModelTogglesSessions(t *testing.T) {
	sessionPage := &testPage{}
	sessionsPage := &testPage{}
	root := newTestModel(sessionPage, sessionsPage)

	updated, _ := root.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	got := updated.(model)

	if got.navigator.Current().Name() != routes.SessionsName {
		t.Fatalf("current route = %q, want sessions", got.navigator.Current().Name())
	}

	if got.navigator.Current().Param(routes.ParamSessionID) != "session-1" {
		t.Fatalf("session ID = %q, want session-1", got.navigator.Current().Param(routes.ParamSessionID))
	}

	updated, _ = got.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	got = updated.(model)

	if got.navigator.Current().Name() != routes.SessionName {
		t.Fatalf("current route = %q, want session", got.navigator.Current().Name())
	}

	if !sessionsPage.closed || sessionPage.closed {
		t.Fatal("Ctrl+P closed the wrong page")
	}
}

func TestModelQuitsAndClosesPages(t *testing.T) {
	sessionPage := &testPage{}
	root := newTestModel(sessionPage, &testPage{})

	_, command := root.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if command == nil {
		t.Fatal("Ctrl+C command is nil")
	}

	if !sessionPage.closed {
		t.Fatal("Ctrl+C did not close the active page")
	}

	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command returned %T, want tea.QuitMsg", command())
	}
}

func TestModelUsesAlternateScreen(t *testing.T) {
	root := newTestModel(&testPage{}, &testPage{})

	view := root.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("View() = AltScreen:%t MouseMode:%v", view.AltScreen, view.MouseMode)
	}
}

func TestModelForwardsTerminalSize(t *testing.T) {
	sessionPage := &testPage{}
	root := newTestModel(sessionPage, &testPage{})

	root.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if sessionPage.width != 120 || sessionPage.height != 40 {
		t.Fatalf("page size = %dx%d, want 120x40", sessionPage.width, sessionPage.height)
	}
}

func newTestModel(sessionPage, sessionsPage *testPage) model {
	return newModel(
		routes.Session("session-1"),
		map[navigation.RouteName]navigation.Factory{
			routes.SessionName:  func(navigation.Route) tea.Model { return sessionPage },
			routes.SessionsName: func(navigation.Route) tea.Model { return sessionsPage },
		},
	)
}

type testPage struct {
	closed bool
	width  int
	height int
}

func (*testPage) Init() tea.Cmd {
	return nil
}

func (p *testPage) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		p.width = size.Width
		p.height = size.Height
	}

	return p, nil
}

func (*testPage) View() tea.View {
	return tea.NewView("test")
}

func (p *testPage) Close() {
	p.closed = true
}
