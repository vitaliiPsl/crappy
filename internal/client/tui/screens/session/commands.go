package session

import (
	"encoding/json"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

const subscriptionStartEntryID uint64 = 0

type modelMsg struct {
	target *Model
	model  service.ModelSelection
	err    error
}

func (modelMsg) Broadcast() {}

type workdirMsg struct {
	target    *Model
	sessionID string
	directory string
	err       error
}

func (workdirMsg) Broadcast() {}

type contextMsg struct {
	target    *Model
	sessionID string
	data      service.ContextReport
	err       error
}

func (contextMsg) Broadcast() {}

type mcpMsg struct {
	target  *Model
	servers []service.MCPServer
	err     error
}

func (mcpMsg) Broadcast() {}

type subscribedMsg struct {
	target       *Model
	subscription event.Subscription
	err          error
}

func (subscribedMsg) Broadcast() {}

type eventMsg struct {
	target *Model
	event  event.Event
	err    error
	open   bool
}

func (eventMsg) Broadcast() {}

type createdMsg struct {
	target    *Model
	sessionID string
	input     kit.Message
	err       error
}

func (createdMsg) Broadcast() {}

type executeResultMsg struct {
	target      *Model
	executionID string
	input       string
	err         error
}

func (executeResultMsg) Broadcast() {}

type executionWaitMsg struct {
	target *Model
	err    error
}

func (executionWaitMsg) Broadcast() {}

type cancelResultMsg struct {
	target *Model
	err    error
}

func (cancelResultMsg) Broadcast() {}

func (m *Model) loadModel() tea.Cmd {
	return func() tea.Msg {
		model, err := m.service.GetModel(m.ctx)

		return modelMsg{target: m, model: model, err: err}
	}
}

func (m *Model) loadWorkdir() tea.Cmd {
	sessionID := m.sessionID

	return func() tea.Msg {
		if sessionID != "" {
			record, err := m.service.GetSession(m.ctx, sessionID)
			if err != nil {
				return workdirMsg{target: m, sessionID: sessionID, err: err}
			}

			if value, found := record.Values[workdir.StateKey]; found {
				var directory string
				if err := json.Unmarshal(value, &directory); err != nil {
					return workdirMsg{target: m, sessionID: sessionID, err: fmt.Errorf("decode session workdir: %w", err)}
				}

				return workdirMsg{target: m, sessionID: sessionID, directory: directory}
			}
		}

		directory, err := os.Getwd()

		return workdirMsg{target: m, sessionID: sessionID, directory: directory, err: err}
	}
}

func (m *Model) subscribe() tea.Cmd {
	return func() tea.Msg {
		subscription, err := m.service.Subscribe(m.ctx, m.sessionID, subscriptionStartEntryID)

		return subscribedMsg{target: m, subscription: subscription, err: err}
	}
}

func (m *Model) waitForEvent() tea.Cmd {
	if m.subscription == nil {
		return nil
	}

	subscription := m.subscription

	return func() tea.Msg {
		value, open := <-subscription.Events()

		return eventMsg{
			target: m,
			event:  value,
			err:    subscription.Err(),
			open:   open,
		}
	}
}

func (m *Model) create(input kit.Message) tea.Cmd {
	return func() tea.Msg {
		opened, err := m.service.CreateSession(m.ctx, "")
		if err != nil {
			return createdMsg{target: m, err: err}
		}

		return createdMsg{target: m, sessionID: opened.ID, input: input}
	}
}

func (m *Model) execute(input kit.Message) tea.Cmd {
	sessionID := m.sessionID
	text := kit.ContentsText(input.Content)

	return func() tea.Msg {
		id, err := m.service.Execute(m.ctx, sessionID, input.Content...)

		return executeResultMsg{target: m, executionID: id, input: text, err: err}
	}
}

func (m *Model) steer(input kit.Message) tea.Cmd {
	sessionID := m.sessionID
	text := kit.ContentsText(input.Content)

	return func() tea.Msg {
		err := m.service.Steer(m.ctx, sessionID, input.Content...)

		return executeResultMsg{target: m, input: text, err: err}
	}
}

func (m *Model) waitForExecution(executionID string) tea.Cmd {
	sessionID := m.sessionID

	return func() tea.Msg {
		_, err := m.service.WaitExecution(m.ctx, sessionID, executionID)

		return executionWaitMsg{target: m, err: err}
	}
}

func (m *Model) cancelExecution(executionID string) tea.Cmd {
	return func() tea.Msg {
		err := m.service.Cancel(m.ctx, m.sessionID, executionID)

		return cancelResultMsg{target: m, err: err}
	}
}
