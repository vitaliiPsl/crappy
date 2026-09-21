package sessions

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

var errCurrentSession = errors.New("current session cannot be deleted")

const (
	previousSession = -1
	nextSession     = 1
)

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	runtime *runtime.Runtime

	currentID string
	sessions  []session.State
	selected  int
	offset    int
	loading   bool
	deleting  bool
	deleteID  string
	err       error

	width  int
	height int
}

func New(ctx context.Context, runtime *runtime.Runtime, currentID string) *Model {
	ctx, cancel := context.WithCancel(ctx)

	return &Model{
		ctx:       ctx,
		cancel:    cancel,
		runtime:   runtime,
		currentID: currentID,
		loading:   true,
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
		m.ensureVisible()
	case loadedMsg:
		m.applyLoaded(message)
	case deletedMsg:
		m.applyDeleted(message)
	case tea.KeyPressMsg:
		return m, m.handleKey(message.String())
	}

	return m, nil
}

func (m *Model) View() tea.View {
	return tea.NewView(m.render())
}

func (m *Model) Close() {
	m.cancel()
}

func (m *Model) handleKey(key string) tea.Cmd {
	if m.deleteID != "" {
		return m.handleDeleteKey(key)
	}

	switch key {
	case "esc":
		return back()
	case "n":
		return openNew()
	case "d":
		m.confirmDelete()
	case "up", "k":
		m.move(previousSession)
	case "down", "j":
		m.move(nextSession)
	case "enter":
		if m.selected < len(m.sessions) {
			return open(m.sessions[m.selected].ID)
		}
	}

	return nil
}

func (m *Model) handleDeleteKey(key string) tea.Cmd {
	if m.deleting {
		return nil
	}

	switch key {
	case "enter":
		m.deleting = true

		return m.deleteSession(m.deleteID)
	case "esc":
		m.deleteID = ""
	}

	return nil
}

func (m *Model) confirmDelete() {
	if m.selected >= len(m.sessions) {
		return
	}

	sessionID := m.sessions[m.selected].ID
	if sessionID == m.currentID {
		m.err = errCurrentSession

		return
	}

	m.err = nil
	m.deleteID = sessionID
}

func (m *Model) move(delta int) {
	if len(m.sessions) == 0 {
		return
	}

	m.selected = min(max(m.selected+delta, 0), len(m.sessions)-1)
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	visible := m.visibleRows()
	if m.selected < m.offset {
		m.offset = m.selected
	}

	if m.selected >= m.offset+visible {
		m.offset = m.selected - visible + 1
	}
}
