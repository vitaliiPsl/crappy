package session

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/commands"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

func (m *Model) newCommands() *commands.Registry {
	return commands.New(
		commands.Command{Name: "help", Description: "Show available commands", Run: m.help},
		commands.Command{Name: "new", Description: "Start a new conversation", Run: m.newSession},
		commands.Command{Name: "sessions", Description: "Browse sessions", Run: m.showSessions},
		commands.Command{Name: "compact", Description: "Compact the conversation", Run: m.compact},
	)
}

func (m *Model) help() tea.Cmd {
	m.notice = notice{kind: noticeInfo, message: m.commands.Help()}

	return nil
}

func (*Model) newSession() tea.Cmd {
	return func() tea.Msg {
		return navigation.Reset(routes.Session(""))
	}
}

func (m *Model) showSessions() tea.Cmd {
	sessionID := m.sessionID

	return func() tea.Msg {
		return navigation.Toggle(routes.Sessions(sessionID))
	}
}

func (m *Model) compact() tea.Cmd {
	sessionID := m.sessionID

	return func() tea.Msg {
		current, err := m.assistant.Compact(m.ctx, sessionID)

		return executeResultMsg{target: m, execution: current, err: err}
	}
}
