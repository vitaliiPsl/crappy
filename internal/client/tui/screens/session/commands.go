package session

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	harnesssession "github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

const subscriptionStartEntryID uint64 = 0

type subscribedMsg struct {
	target       *Model
	subscription *event.Subscription
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
	target *Model
	state  harnesssession.State
	input  kit.Message
	err    error
}

func (createdMsg) Broadcast() {}

type executeResultMsg struct {
	target *Model
	err    error
}

func (executeResultMsg) Broadcast() {}

type cancelResultMsg struct {
	target *Model
	err    error
}

func (cancelResultMsg) Broadcast() {}

func (m *Model) subscribe() tea.Cmd {
	return func() tea.Msg {
		subscription, err := m.runtime.Subscribe(
			m.ctx,
			m.sessionID,
			subscriptionStartEntryID,
		)

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
		state, err := m.runtime.Create(m.ctx, runtime.CreateOptions{Title: inputTitle(input)})

		return createdMsg{target: m, state: state, input: input, err: err}
	}
}

func (m *Model) execute(input kit.Message) tea.Cmd {
	return func() tea.Msg {
		_, err := m.runtime.Execute(m.ctx, m.sessionID, input)

		return executeResultMsg{target: m, err: err}
	}
}

func (m *Model) cancelExecution(executionID string) tea.Cmd {
	return func() tea.Msg {
		err := m.runtime.Cancel(m.ctx, m.sessionID, executionID)

		return cancelResultMsg{target: m, err: err}
	}
}
