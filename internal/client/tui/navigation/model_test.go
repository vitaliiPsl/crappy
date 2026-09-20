package navigation

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

const testRouteName RouteName = "test"

func testRoute() Route {
	return NewRoute(testRouteName, nil)
}

func TestPushKeepsCurrentPageAlive(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)
	navigator.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	navigator.Update(Push(testRoute()))

	if len(navigator.stack) != 2 || navigator.stack[0].page != current {
		t.Fatal("Push() did not retain the current page")
	}

	if current.closed {
		t.Fatal("Push() closed the current page")
	}

	if !next.initialized || next.width != 120 || next.height != 40 {
		t.Fatalf("pushed page = %+v, want initialized at 120x40", next)
	}
}

func TestBackRestoresPreviousPage(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)
	navigator.Update(Push(testRoute()))

	navigator.Update(Back())

	if len(navigator.stack) != 1 || navigator.View().Content != current.View().Content {
		t.Fatal("Back() did not restore the previous page")
	}

	if !next.closed || current.closed {
		t.Fatal("Back() closed the wrong page")
	}
}

func TestResetClosesStack(t *testing.T) {
	first := &testPage{}
	second := &testPage{}
	replacement := &testPage{}
	factories := map[RouteName]Factory{
		testRouteName: func(route Route) tea.Model {
			if route.Param("id") == "replacement" {
				return replacement
			}

			return first
		},
		"second": func(Route) tea.Model { return second },
	}
	navigator := New(NewRoute(testRouteName, map[string]string{"id": "first"}), factories)
	navigator.Update(Push(NewRoute("second", nil)))

	navigator.Update(Reset(NewRoute(testRouteName, map[string]string{"id": "replacement"})))

	if !first.closed || !second.closed {
		t.Fatal("Reset() did not close the existing pages")
	}

	if len(navigator.stack) != 1 || navigator.stack[0].page != replacement {
		t.Fatal("Reset() did not install the replacement page")
	}
}

func TestTogglePushesAndReturns(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)

	navigator.Update(Toggle(testRoute()))

	if len(navigator.stack) != 2 {
		t.Fatal("first Toggle() did not push the route")
	}

	navigator.Update(Toggle(testRoute()))

	if len(navigator.stack) != 1 || !next.closed {
		t.Fatal("second Toggle() did not return from the route")
	}
}

func TestInputOnlyReachesActivePage(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)
	navigator.Update(Push(testRoute()))

	current.messages = nil
	next.messages = nil

	navigator.Update(tea.KeyPressMsg(tea.Key{Code: 'j'}))
	navigator.Update(struct{}{})
	navigator.Update(tea.PasteMsg{Content: "pasted"})

	if len(current.messages) != 0 || len(next.messages) != 3 {
		t.Fatalf("messages = current:%d active:%d, want 0 and 3", len(current.messages), len(next.messages))
	}
}

func TestAsyncMessagesReachEveryPage(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)
	navigator.Update(Push(testRoute()))

	current.messages = nil
	next.messages = nil

	navigator.Update(broadcastMsg{})

	if len(current.messages) != 1 || len(next.messages) != 1 {
		t.Fatalf("messages = current:%d active:%d, want 1 and 1", len(current.messages), len(next.messages))
	}
}

func TestCloseClosesEveryPage(t *testing.T) {
	current := &testPage{}
	next := &testPage{}
	navigator := testNavigator(current, next)
	navigator.Update(Push(testRoute()))

	navigator.Close()

	if !current.closed || !next.closed || len(navigator.stack) != 0 {
		t.Fatal("Close() did not close and remove every page")
	}
}

func TestRoutesOwnParams(t *testing.T) {
	params := map[string]string{"id": "value"}
	route := NewRoute(testRouteName, params)
	params["id"] = "changed"

	if route.Name() != testRouteName || route.Param("id") != "value" {
		t.Fatalf("route = %q/%q, want test/value", route.Name(), route.Param("id"))
	}
}

func testNavigator(current, next *testPage) *Model {
	return New(NewRoute("current", nil), map[RouteName]Factory{
		"current":     func(Route) tea.Model { return current },
		testRouteName: func(Route) tea.Model { return next },
	})
}

type broadcastMsg struct{}

func (broadcastMsg) Broadcast() {}

type testPage struct {
	initialized bool
	closed      bool
	messages    []tea.Msg
	width       int
	height      int
}

func (p *testPage) Init() tea.Cmd {
	p.initialized = true

	return nil
}

func (p *testPage) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	p.messages = append(p.messages, message)

	if size, ok := message.(tea.WindowSizeMsg); ok {
		p.width = size.Width
		p.height = size.Height
	}

	return p, nil
}

func (p *testPage) View() tea.View {
	return tea.NewView("test")
}

func (p *testPage) Close() {
	p.closed = true
}
