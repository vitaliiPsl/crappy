package transcript

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Model struct {
	viewport   viewport.Model
	executions []*execution
	width      int
	height     int

	content string
	view    string
}

func New() Model {
	viewport := viewport.New()
	viewport.SoftWrap = true

	return Model{viewport: viewport}
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	var command tea.Cmd

	m.viewport, command = m.viewport.Update(message)
	m.view = ""

	return command
}

func (m *Model) View() string {
	if content := m.render(); content != m.content {
		m.content = content
		m.view = ""

		follow := m.viewport.AtBottom()
		m.viewport.SetContent(content)

		if follow {
			m.viewport.GotoBottom()
		}
	}

	if m.view == "" {
		m.view = m.viewport.View()
	}

	return m.view
}

func (m *Model) SetSize(width, height int) {
	width = max(width, 1)
	height = max(height, 1)

	if width == m.width && height == m.height {
		return
	}

	m.width = width
	m.height = height
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.height)
	m.view = ""
}

func (m *Model) Start(executionID string) {
	m.execution(executionID)
}

func (m *Model) Append(executionID string, message kit.Message) {
	m.execution(executionID).append(message)
}

func (m *Model) Apply(executionID string, event kit.AgentEvent) {
	m.execution(executionID).apply(event)
}

func (m *Model) Finish(executionID string) {
	m.execution(executionID).finish()
}

func (m *Model) execution(id string) *execution {
	if len(m.executions) > 0 {
		last := m.executions[len(m.executions)-1]
		if last.id == id {
			return last
		}
	}

	created := newExecution(id)
	m.executions = append(m.executions, created)

	return created
}

func (m *Model) render() string {
	sections := make([]string, 0, len(m.executions))
	for _, value := range m.executions {
		if rendered := value.render(m.width); rendered != "" {
			sections = append(sections, rendered)
		}
	}

	if len(sections) == 0 {
		return renderEmpty(m.width, m.height)
	}

	return strings.Join(sections, sectionSeparator)
}
