package sessions

import (
	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

type loadedMsg struct {
	target   *Model
	sessions []session.State
	err      error
}

type deletedMsg struct {
	target    *Model
	sessionID string
	err       error
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.runtime.Sessions(m.ctx)

		return loadedMsg{target: m, sessions: sessions, err: err}
	}
}

func (m *Model) deleteSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := m.runtime.DeleteSession(m.ctx, sessionID)

		return deletedMsg{target: m, sessionID: sessionID, err: err}
	}
}

func open(sessionID string) tea.Cmd {
	return func() tea.Msg {
		return navigation.Reset(routes.Session(sessionID))
	}
}

func openNew() tea.Cmd {
	return open(uuid.NewString())
}

func back() tea.Cmd {
	return func() tea.Msg {
		return navigation.Back()
	}
}
