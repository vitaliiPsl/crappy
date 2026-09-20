package session

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/composer"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/transcript"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	runtime      *runtime.Runtime
	subscription *event.Subscription

	sessionID string
	err       error

	transcript transcript.Model
	composer   composer.Model
	width      int
	height     int
}

func New(ctx context.Context, runtime *runtime.Runtime, sessionID string) *Model {
	ctx, cancel := context.WithCancel(ctx)

	return &Model{
		ctx:        ctx,
		cancel:     cancel,
		runtime:    runtime,
		sessionID:  sessionID,
		transcript: transcript.New(),
		composer:   composer.New(),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.composer.Init(), m.subscribe())
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.resize()

		return m, nil
	case subscribedMsg:
		return m, m.handleSubscribed(message)
	case eventMsg:
		return m, m.handleEvent(message)
	case executeResultMsg:
		m.handleExecuteResult(message)

		return m, nil
	case composer.Submit:
		return m, m.submit(message.Text)
	case tea.KeyPressMsg:
		if isTranscriptKey(message.String()) {
			return m, m.transcript.Update(message)
		}
	case tea.MouseWheelMsg:
		return m, m.transcript.Update(message)
	}

	previousHeight := m.composer.Height()

	command := m.composer.Update(message)

	if previousHeight != m.composer.Height() {
		m.resize()
	}

	return m, command
}

func (m *Model) View() tea.View {
	return tea.NewView(strings.Join([]string{
		m.sessionID,
		m.transcript.View(),
		m.status(),
		m.composer.View(),
	}, "\n"))
}

func (m *Model) Close() {
	m.cancel()

	if m.subscription != nil {
		m.subscription.Close()
	}
}
