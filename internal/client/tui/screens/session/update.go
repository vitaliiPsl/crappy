package session

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	harnesssession "github.com/vitaliipsl/crappy/harness/session"
)

const cancelKey = "esc"

func (m *Model) handleWorkdir(message workdirMsg) {
	if message.target != m || message.sessionID != m.sessionID || m.ctx.Err() != nil {
		return
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return
	}

	m.workdir = message.directory
}

func (m *Model) submit(text string) tea.Cmd {
	if m.running() || m.terminalErr != nil {
		return nil
	}

	m.notice = notice{}
	input := kit.NewUserMessage(kit.NewTextContent(text))

	if m.sessionID == "" {
		return m.create(input)
	}

	return m.execute(input)
}

func (m *Model) handleCreated(message createdMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return nil
	}

	m.sessionID = message.sessionID

	return tea.Batch(m.loadWorkdir(), m.subscribe(), m.execute(message.input))
}

func (m *Model) handleSubscribed(message subscribedMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return nil
	}

	m.subscription = message.subscription

	return m.waitForEvent()
}

func (m *Model) handleEvent(message eventMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if !message.open {
		m.subscription = nil
		if errors.Is(message.err, event.ErrClosed) {
			m.executionID = ""
		}

		if message.err != nil {
			m.notice = errorNotice(message.err)
		}

		return nil
	}

	m.applyEvent(message.event)

	return m.waitForEvent()
}

func (m *Model) handleExecuteResult(message executeResultMsg) tea.Cmd {
	if message.target != m || m.ctx.Err() != nil {
		return nil
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return nil
	}

	m.composer.Reset()

	return m.waitForExecution(message.execution)
}

func (m *Model) handleExecutionWait(message executionWaitMsg) {
	if message.target != m || m.ctx.Err() != nil || message.err == nil {
		return
	}

	m.executionID = ""
	m.terminalErr = message.err
}

func (m *Model) handleCancelResult(message cancelResultMsg) {
	if message.target != m {
		return
	}

	if message.err == nil || errors.Is(message.err, harnesssession.ErrExecutionNotActive) {
		return
	}

	m.notice = errorNotice(message.err)
}

func isTranscriptKey(key string) bool {
	switch key {
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		return true
	default:
		return false
	}
}
