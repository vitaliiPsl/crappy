package session

import (
	"encoding/json"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/interaction"
	promptview "github.com/vitaliipsl/crappy/internal/client/tui/components/prompt"
)

type promptResponseMsg struct {
	target    *Model
	requestID string
	err       error
}

func (promptResponseMsg) Broadcast() {}

func (m *Model) applyPromptEvent(value event.Event) tea.Cmd {
	switch value.Type {
	case interaction.RequestedEntryType:
		var request interaction.Request
		if err := json.Unmarshal(value.Payload, &request); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		m.composer.Blur()

		return m.prompt.Add(request)

	case interaction.AnsweredEntryType:
		var response interaction.Response
		if err := json.Unmarshal(value.Payload, &response); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		return m.removePrompt(response.RequestID)
	case interaction.CancelledEntryType:
		var requestID string
		if err := json.Unmarshal(value.Payload, &requestID); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		return m.removePrompt(requestID)
	}

	return nil
}

func (m *Model) removePrompt(requestID string) tea.Cmd {
	command := m.prompt.Remove(requestID)
	if !m.prompt.Active() {
		return m.composer.Focus()
	}

	return command
}

func (m *Model) respond(message promptview.Submit) tea.Cmd {
	if m.terminalErr != nil || !m.prompt.BeginSubmit(message.RequestID) {
		return nil
	}

	sessionID := m.sessionID

	return func() tea.Msg {
		err := m.service.Respond(m.ctx, sessionID, message.RequestID, message.Answer)

		return promptResponseMsg{target: m, requestID: message.RequestID, err: err}
	}
}

func (m *Model) handlePromptResponse(message promptResponseMsg) tea.Cmd {
	if message.target != m || m.ctx.Err() != nil {
		return nil
	}

	m.prompt.EndSubmit(message.requestID)

	if message.err == nil || errors.Is(message.err, interaction.ErrNotFound) {
		return m.removePrompt(message.requestID)
	}

	m.notice = errorNotice(message.err)

	return nil
}
