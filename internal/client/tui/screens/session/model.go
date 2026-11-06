package session

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/commands"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/composer"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/transcript"
)

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	assistant    *assistant.Assistant
	subscription event.Subscription
	commands     *commands.Registry

	sessionID      string
	workdir        string
	pending        []execution.Record
	submission     string
	contextLoading bool
	notice         notice
	terminalErr    error

	sessionUsage kit.Usage
	model        assistant.ModelSelection

	transcript transcript.Model
	composer   composer.Model
	width      int
	height     int

	queueOpen     bool
	queueSelected int
	queueOffset   int
}

func New(ctx context.Context, app *assistant.Assistant, sessionID string) *Model {
	ctx, cancel := context.WithCancel(ctx)

	model := &Model{
		ctx:        ctx,
		cancel:     cancel,
		assistant:  app,
		sessionID:  sessionID,
		transcript: transcript.New(),
		composer:   composer.New(),
	}
	model.commands = model.newCommands()

	return model
}

func (m *Model) Init() tea.Cmd {
	if m.sessionID == "" {
		return tea.Batch(m.composer.Init(), m.loadModel(), m.loadWorkdir())
	}

	return tea.Batch(m.composer.Init(), m.loadModel(), m.loadWorkdir(), m.subscribe())
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height

		return m, nil
	case modelMsg:
		m.handleModel(message)

		return m, nil
	case workdirMsg:
		m.handleWorkdir(message)

		return m, nil
	case contextMsg:
		m.handleContext(message)

		return m, nil
	case subscribedMsg:
		return m, m.handleSubscribed(message)
	case eventMsg:
		return m, m.handleEvent(message)
	case createdMsg:
		return m, m.handleCreated(message)
	case executeResultMsg:
		return m, m.handleExecuteResult(message)
	case executionWaitMsg:
		m.handleExecutionWait(message)

		return m, nil
	case cancelResultMsg:
		m.handleCancelResult(message)

		return m, nil
	case composer.Submit:
		if m.queueOpen {
			return m, nil
		}

		return m, m.submit(message.Text)
	case tea.MouseWheelMsg:
		if m.queueOpen {
			switch message.Button {
			case tea.MouseWheelUp:
				m.moveQueue(-1)
			case tea.MouseWheelDown:
				m.moveQueue(1)
			}

			return m, nil
		}

		return m, m.transcript.Update(message)
	case tea.KeyPressMsg:
		if m.queueOpen {
			return m, m.handleQueueKey(message.String())
		}

		if message.String() == cancelKey && m.running() {
			return m, m.cancelExecution(m.pending[0].ID)
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
	return len(m.pending) > 0 && m.pending[0].Status == execution.Running && m.terminalErr == nil
}
