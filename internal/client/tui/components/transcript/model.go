package transcript

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Model struct {
	viewport   viewport.Model
	projection projection
	width      int
	height     int
}

func New() Model {
	viewport := viewport.New()
	viewport.SoftWrap = true

	return Model{
		viewport:   viewport,
		projection: newProjection(),
	}
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	var command tea.Cmd

	m.viewport, command = m.viewport.Update(message)

	return command
}

func (m Model) View() string {
	return m.viewport.View()
}

func (m *Model) SetSize(width, height int) {
	width = max(width, 1)
	height = max(height, 1)

	if width == m.width && height == m.height {
		return
	}

	follow := m.viewport.AtBottom()

	m.width = width
	m.height = height
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.height)
	m.refresh()

	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *Model) Append(message kit.Message) {
	m.projection.append(message)
	m.refreshAndFollow()
}

func (m *Model) Apply(event kit.AgentEvent) {
	m.projection.apply(event)

	m.refreshAndFollow()
}

func (m *Model) refreshAndFollow() {
	follow := m.viewport.AtBottom()
	m.refresh()

	if follow {
		m.viewport.GotoBottom()
	}
}
