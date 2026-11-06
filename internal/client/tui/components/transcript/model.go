package transcript

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/compaction"
)

type block interface {
	render(width int) string
}

type Model struct {
	viewport      viewport.Model
	blocks        []block
	lastExecution *execution
	width         int
	height        int

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
	m.execution(executionID).status = executionRunning
}

func (m *Model) Append(executionID string, message kit.Message) {
	m.execution(executionID).append(message)
}

// AppendCommand appends styled TUI output; callers must sanitize external text.
func (m *Model) AppendCommand(name, output string) {
	m.blocks = append(m.blocks, newCommandOutput(name, output))
}

func (m *Model) Apply(executionID string, event kit.AgentEvent) {
	m.execution(executionID).apply(event)
}

func (m *Model) StartCompaction(executionID string) {
	m.execution(executionID).startCompaction()
}

func (m *Model) AppendCompaction(executionID string, generated compaction.Compaction) {
	m.execution(executionID).appendCompaction(generated)
}

func (m *Model) Finish(executionID string) {
	m.execution(executionID).finish()
}

func (m *Model) execution(id string) *execution {
	if m.lastExecution != nil && m.lastExecution.id == id {
		return m.lastExecution
	}

	created := newExecution(id)
	m.blocks = append(m.blocks, created)
	m.lastExecution = created

	return created
}

func (m *Model) render() string {
	sections := make([]string, 0, len(m.blocks))
	for _, value := range m.blocks {
		if rendered := value.render(m.width); rendered != "" {
			sections = append(sections, rendered)
		}
	}

	if len(sections) == 0 {
		return renderEmpty(m.width, m.height)
	}

	return strings.Join(sections, sectionSeparator)
}
