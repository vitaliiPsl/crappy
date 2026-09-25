package session

import (
	"context"

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

	sessionID   string
	executionID string
	notice      notice

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
	if m.sessionID == "" {
		return m.composer.Init()
	}

	return tea.Batch(m.composer.Init(), m.subscribe())
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height

		return m, nil
	case subscribedMsg:
		return m, m.handleSubscribed(message)
	case eventMsg:
		return m, m.handleEvent(message)
	case createdMsg:
		return m, m.handleCreated(message)
	case executeResultMsg:
		m.handleExecuteResult(message)

		return m, nil
	case cancelResultMsg:
		m.handleCancelResult(message)

		return m, nil
	case composer.Submit:
		return m, m.submit(message.Text)
	case tea.MouseWheelMsg:
		return m, m.transcript.Update(message)
	case tea.KeyPressMsg:
		if message.String() == cancelKey && m.running() {
			return m, m.cancelExecution(m.executionID)
		}

		if isTranscriptKey(message.String()) {
			return m, m.transcript.Update(message)
		}
	}

	return m, m.composer.Update(message)
}

func (m *Model) View() tea.View {
	return tea.NewView(m.render())
}

func (m *Model) Close() {
	m.cancel()

	if m.subscription != nil {
		m.subscription.Close()
	}
}

func (m *Model) running() bool {
	return m.executionID != ""
}
