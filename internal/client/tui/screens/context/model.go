package contextview

import (
	"context"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
)

type viewMode uint8

const (
	overviewView viewMode = iota
	messagesView
	toolsView
	detailView
)

type viewState struct {
	mode     viewMode
	selected int
	offset   int
}

type Model struct {
	ctx       context.Context
	cancel    context.CancelFunc
	assistant *assistant.Assistant
	sessionID string
	data      assistant.ContextReport
	loading   bool
	err       error

	mode     viewMode
	selected int
	offset   int
	history  []viewState

	detailTitle  string
	detailTokens int
	viewport     viewport.Model
	width        int
	height       int
}

func New(ctx context.Context, app *assistant.Assistant, sessionID string) *Model {
	ctx, cancel := context.WithCancel(ctx)
	viewport := viewport.New()
	viewport.SoftWrap = true

	return &Model{
		ctx:       ctx,
		cancel:    cancel,
		assistant: app,
		sessionID: sessionID,
		loading:   true,
		viewport:  viewport,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.load()
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.resize()
	case loadedMsg:
		if message.target == m && m.ctx.Err() == nil {
			m.loading = false
			m.err = message.err
			m.data = message.data
			m.resize()
		}
	case tea.KeyPressMsg:
		return m, m.handleKey(message)
	case tea.MouseWheelMsg:
		if m.mode == detailView {
			var command tea.Cmd

			m.viewport, command = m.viewport.Update(message)

			return m, command
		}
	}

	return m, nil
}

func (m *Model) View() tea.View {
	return tea.NewView(m.render())
}

func (m *Model) Close() {
	m.cancel()
}

func (m *Model) handleKey(message tea.KeyPressMsg) tea.Cmd {
	switch message.String() {
	case "esc":
		if len(m.history) == 0 {
			return func() tea.Msg { return navigation.Back() }
		}

		previous := m.history[len(m.history)-1]
		m.history = m.history[:len(m.history)-1]
		m.mode, m.selected, m.offset = previous.mode, previous.selected, previous.offset
		m.resize()

		return nil
	case "r":
		if !m.loading {
			return m.load()
		}

		return nil
	}

	if m.loading || m.err != nil {
		return nil
	}

	if m.mode == detailView {
		var command tea.Cmd

		m.viewport, command = m.viewport.Update(message)

		return command
	}

	switch message.String() {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-m.bodyHeight())
	case "pgdown", "ctrl+d":
		m.move(m.bodyHeight())
	case "home":
		m.move(-len(m.items()))
	case "end":
		m.move(len(m.items()))
	case "enter":
		m.inspect()
	}

	return nil
}

func (m *Model) move(delta int) {
	m.selected = min(max(m.selected+delta, 0), max(len(m.items())-1, 0))
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	if m.selected < m.offset {
		m.offset = m.selected
	}

	if m.selected >= m.offset+m.bodyHeight() {
		m.offset = m.selected - m.bodyHeight() + 1
	}
}

func (m *Model) resize() {
	m.viewport.SetWidth(m.contentWidth())
	m.viewport.SetHeight(m.bodyHeight())
	m.ensureVisible()
}

func (m *Model) open(mode viewMode) {
	m.history = append(m.history, viewState{mode: m.mode, selected: m.selected, offset: m.offset})
	m.mode = mode
	m.selected = 0
	m.offset = 0
}

func (m *Model) showDetail(title, content string, tokens int) {
	m.open(detailView)
	m.detailTitle = title
	m.detailTokens = tokens
	m.resize()
	m.viewport.SetContent(ansi.Strip(content))
	m.viewport.GotoTop()
}
