package transcript

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Model struct {
	viewport  viewport.Model
	messages  []kit.Message
	streaming []kit.Content
	active    int
}

func New() Model {
	viewport := viewport.New()
	viewport.SoftWrap = true

	return Model{viewport: viewport, active: -1}
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
	m.viewport.SetWidth(max(width, 1))
	m.viewport.SetHeight(max(height, 1))
	m.refresh()
}

func (m *Model) Append(message kit.Message) {
	if message.Role == kit.RoleModel {
		m.streaming = nil
		m.active = -1
	}

	m.messages = append(m.messages, message)
	m.refresh()
	m.viewport.GotoBottom()
}
