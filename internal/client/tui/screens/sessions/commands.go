package sessions

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

type loadedMsg struct {
	target   *Model
	sessions []state.Record
	err      error
}

type deletedMsg struct {
	target    *Model
	sessionID string
	err       error
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.assistant.Sessions(m.ctx)

		return loadedMsg{target: m, sessions: sessions, err: err}
	}
}

func (m *Model) deleteSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := m.assistant.DeleteSession(m.ctx, sessionID)

		return deletedMsg{target: m, sessionID: sessionID, err: err}
	}
}

func open(sessionID string) tea.Cmd {
	return func() tea.Msg {
		return navigation.Reset(routes.Session(sessionID))
	}
}

func openNew() tea.Cmd {
	return open("")
}

func back() tea.Cmd {
	return func() tea.Msg {
		return navigation.Back()
	}
}
