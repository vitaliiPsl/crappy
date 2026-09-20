package session

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func (m *Model) submit(text string) tea.Cmd {
	m.err = nil
	m.resize()

	return m.execute(kit.NewUserMessage(kit.NewTextContent(text)))
}

func (m *Model) handleSubscribed(message subscribedMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if message.err != nil {
		m.err = message.err
		m.resize()

		return nil
	}

	m.subscription = message.subscription
	m.err = nil
	m.resize()

	return m.waitForEvent()
}

func (m *Model) handleEvent(message eventMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if !message.open {
		m.subscription = nil
		m.err = message.err
		m.resize()

		return nil
	}

	m.applyEvent(message.event)
	m.resize()

	return m.waitForEvent()
}

func (m *Model) handleExecuteResult(message executeResultMsg) {
	if message.target != m {
		return
	}

	if message.err != nil {
		m.err = message.err
		m.resize()

		return
	}

	m.composer.Reset()
	m.resize()
}

func isTranscriptKey(key string) bool {
	switch key {
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		return true
	default:
		return false
	}
}
