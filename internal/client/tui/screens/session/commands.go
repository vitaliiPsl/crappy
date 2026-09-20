package session

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
)

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

type executeResultMsg struct {
	target *Model
	err    error
}

func (executeResultMsg) Broadcast() {}

func (m *Model) subscribe() tea.Cmd {
	return func() tea.Msg {
		subscription, err := m.runtime.Subscribe(m.ctx, m.sessionID, 0)

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

func (m *Model) execute(input kit.Message) tea.Cmd {
	return func() tea.Msg {
		_, err := m.runtime.Execute(m.ctx, m.sessionID, input)

		return executeResultMsg{target: m, err: err}
	}
}
