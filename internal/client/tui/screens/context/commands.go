package contextview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/internal/assistant"
)

type loadedMsg struct {
	target *Model
	data   assistant.ContextReport
	err    error
}

func (loadedMsg) Broadcast() {}

func (m *Model) load() tea.Cmd {
	m.loading = true
	m.err = nil
	m.mode = overviewView
	m.selected = 0
	m.offset = 0
	m.history = nil

	return func() tea.Msg {
		data, err := m.assistant.Context(m.ctx, m.sessionID)

		return loadedMsg{target: m, data: data, err: err}
	}
}
