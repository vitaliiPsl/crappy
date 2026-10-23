package session

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
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
	target    *Model
	sessionID string
	input     kit.Message
	err       error
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
		opened, err := m.harness.Session(m.ctx, m.sessionID)
		if err != nil {
			return subscribedMsg{target: m, err: err}
		}

		subscription, err := opened.Subscribe(m.ctx, subscriptionStartEntryID)

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
		opened, err := m.harness.Session(m.ctx, "")
		if err != nil {
			return createdMsg{target: m, err: err}
		}

		return createdMsg{target: m, sessionID: opened.ID(), input: input}
	}
}

func (m *Model) execute(input kit.Message) tea.Cmd {
	return func() tea.Msg {
		opened, err := m.harness.Session(m.ctx, m.sessionID)
		if err != nil {
			return executeResultMsg{target: m, err: err}
		}

		_, err = opened.Execute(m.ctx, execution.Request{Input: input})

		return executeResultMsg{target: m, err: err}
	}
}

func (m *Model) cancelExecution(executionID string) tea.Cmd {
	return func() tea.Msg {
		opened, err := m.harness.Session(m.ctx, m.sessionID)
		if err != nil {
			return cancelResultMsg{target: m, err: err}
		}

		err = opened.Cancel(m.ctx, executionID)

		return cancelResultMsg{target: m, err: err}
	}
}
