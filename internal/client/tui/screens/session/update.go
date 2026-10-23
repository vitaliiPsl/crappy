package session

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	harnesssession "github.com/vitaliipsl/crappy/harness/session"
)

const cancelKey = "esc"

func (m *Model) submit(text string) tea.Cmd {
	if m.running() {
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

	return tea.Batch(m.subscribe(), m.execute(message.input))
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

		if message.err != nil {
			m.notice = errorNotice(message.err)
		}

		return nil
	}

	m.applyEvent(message.event)

	return m.waitForEvent()
}

func (m *Model) handleExecuteResult(message executeResultMsg) {
	if message.target != m {
		return
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return
	}

	m.composer.Reset()
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
