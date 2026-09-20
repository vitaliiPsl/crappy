package session

import (
	"encoding/json"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/memory"
	harnesssession "github.com/vitaliipsl/crappy/harness/session"
)

func (m *Model) applyEvent(value event.Event) {
	switch value.Type {
	case memory.EntryType:
		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			m.err = err

			return
		}

		m.transcript.Append(message)
	case harnesssession.AgentEventType:
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			m.err = err

			return
		}

		m.transcript.Apply(agentEvent)
	}
}
