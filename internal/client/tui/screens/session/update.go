package session

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
)

const (
	cancelKey = "esc"
	steerKey  = "alt+enter"
)

func (m *Model) handleModel(message modelMsg) {
	if message.target != m || m.ctx.Err() != nil {
		return
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return
	}

	m.model = message.model
}

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

func (m *Model) handleOutput(message outputMsg) {
	if message.target != m || m.ctx.Err() != nil {
		return
	}

	delete(m.loading, message.command)

	if message.sessionID != m.sessionID {
		return
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return
	}

	m.notice = notice{}
	m.transcript.AppendCommand(message.command, message.output)
}

func (m *Model) submit(text string) tea.Cmd {
	command, handled, err := m.commands.Execute(text)
	if handled {
		if err != nil {
			m.notice = errorNotice(err)
		} else {
			m.composer.Reset()
		}

		return command
	}

	if m.submission != "" || m.terminalErr != nil {
		return nil
	}

	m.notice = notice{}
	m.submission = text
	input := kit.NewUserMessage(kit.NewTextContent(text))

	if m.sessionID == "" {
		return m.create(input)
	}

	return m.execute(input)
}

func (m *Model) submitSteering() tea.Cmd {
	text := strings.TrimSpace(m.composer.Value())
	if text == "" || m.submission != "" {
		return nil
	}

	m.notice = notice{}
	m.submission = text

	return m.steer(kit.NewUserMessage(kit.NewTextContent(text)))
}

func (m *Model) handleCreated(message createdMsg) tea.Cmd {
	if message.target != m {
		return nil
	}

	if message.err != nil {
		m.submission = ""
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
		if errors.Is(message.err, event.ErrClosed) && m.terminalErr == nil {
			m.terminalErr = message.err
		}

		if message.err != nil {
			m.notice = errorNotice(message.err)
		}

		return nil
	}

	command := m.applyEvent(message.event)

	return tea.Batch(command, m.waitForEvent())
}

func (m *Model) handleExecuteResult(message executeResultMsg) tea.Cmd {
	if message.target != m || m.ctx.Err() != nil {
		return nil
	}

	if message.input != "" {
		m.submission = ""
		if message.err == nil && strings.TrimSpace(m.composer.Value()) == message.input {
			m.composer.Reset()
		}
	}

	if message.err != nil {
		m.notice = errorNotice(message.err)

		return nil
	}

	if message.executionID == "" {
		return nil
	}

	return m.waitForExecution(message.executionID)
}

func (m *Model) handleExecutionWait(message executionWaitMsg) {
	if message.target != m || m.ctx.Err() != nil || message.err == nil {
		return
	}

	m.terminalErr = message.err
}

func (m *Model) handleCancelResult(message cancelResultMsg) {
	if message.target != m {
		return
	}

	if message.err == nil {
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
