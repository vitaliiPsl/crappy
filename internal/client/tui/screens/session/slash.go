package session

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/client/tui/commands"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/report"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

func (m *Model) newCommands() *commands.Registry {
	return commands.New(
		commands.Command{Name: "help", Description: "Show available commands", Run: m.help},
		commands.Command{Name: "new", Description: "Start a new conversation", Run: m.newSession},
		commands.Command{Name: "sessions", Description: "Browse sessions", Run: m.showSessions},
		commands.Command{Name: "compact", Description: "Compact the conversation", Run: m.compact},
		commands.Command{Name: "context", Description: "Inspect current agent context", Run: m.showContext},
		commands.Command{Name: "mcp", Description: "Show MCP server status", Run: m.showMCP},
		commands.Command{Name: "queue", Description: "Inspect and cancel pending work", Run: m.showQueue},
		commands.Command{Name: "resume", Description: "Continue restored work", Run: m.resume},
	)
}

func (m *Model) help() tea.Cmd {
	m.notice = notice{}
	m.transcript.AppendCommand("help", ansi.Strip(m.commands.Help()))

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

func (m *Model) showContext() tea.Cmd {
	return m.showOutput("context", func(ctx context.Context, sessionID string) (string, error) {
		data, err := m.service.Context(ctx, sessionID)
		if err != nil {
			return "", err
		}

		return report.Context(data), nil
	})
}

func (m *Model) showMCP() tea.Cmd {
	return m.showOutput("mcp", func(ctx context.Context, _ string) (string, error) {
		servers, err := m.service.ListMCPServers(ctx)
		if err != nil {
			return "", err
		}

		return report.MCP(servers), nil
	})
}

func (m *Model) showOutput(command string, load func(ctx context.Context, sessionID string) (string, error)) tea.Cmd {
	if m.loading[command] {
		return nil
	}

	m.loading[command] = true
	sessionID := m.sessionID

	return func() tea.Msg {
		output, err := load(m.ctx, sessionID)

		return outputMsg{target: m, sessionID: sessionID, command: command, output: output, err: err}
	}
}

func (m *Model) compact() tea.Cmd {
	if m.terminalErr != nil {
		return nil
	}

	sessionID := m.sessionID

	return func() tea.Msg {
		id, err := m.service.Compact(m.ctx, sessionID)

		return executeResultMsg{target: m, executionID: id, err: err}
	}
}

func (m *Model) resume() tea.Cmd {
	if m.terminalErr != nil {
		return nil
	}

	sessionID := m.sessionID

	return func() tea.Msg {
		id, err := m.service.Resume(m.ctx, sessionID)

		return executeResultMsg{target: m, executionID: id, err: err}
	}
}
