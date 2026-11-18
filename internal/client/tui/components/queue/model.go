package queue

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/execution"
)

const headingHeight = 2

type Cancel struct {
	ExecutionID string
}

type Model struct {
	entries  []execution.Record
	open     bool
	selected int
	offset   int
	width    int
	height   int
}

func New() Model {
	return Model{width: 80, height: 16}
}

func (m Model) Opened() bool {
	return m.open
}

func (m *Model) Open() {
	m.open = true
	m.clamp()
}

func (m Model) Selected() execution.Record {
	if m.selected >= len(m.entries) {
		return execution.Record{}
	}

	return m.entries[m.selected]
}

func (m *Model) SetEntries(entries []execution.Record) {
	selectedID := m.Selected().ID
	m.entries = slices.Clone(entries)

	if index := slices.IndexFunc(m.entries, func(entry execution.Record) bool { return entry.ID == selectedID }); index >= 0 {
		m.selected = index
	}

	m.clamp()
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.clamp()
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	switch message := message.(type) {
	case tea.KeyPressMsg:
		switch message.String() {
		case "esc":
			m.open = false
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup", "ctrl+u":
			m.move(-m.rows())
		case "pgdown", "ctrl+d":
			m.move(m.rows())
		case "d":
			if selected := m.Selected(); selected.ID != "" {
				return func() tea.Msg { return Cancel{ExecutionID: selected.ID} }
			}
		}
	case tea.MouseWheelMsg:
		switch message.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
	}

	return nil
}

func (m *Model) move(delta int) {
	m.selected += delta
	m.clamp()
}

func (m *Model) clamp() {
	m.selected = max(0, min(m.selected, len(m.entries)-1))
	rows := m.rows()

	m.offset = min(m.offset, max(0, len(m.entries)-rows), m.selected)
	if m.selected >= m.offset+rows {
		m.offset = m.selected - rows + 1
	}
}

func (m Model) rows() int {
	return max(1, m.height-headingHeight)
}
