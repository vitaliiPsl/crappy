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
			m.notice = errorNotice(err)

			return
		}

		m.transcript.Append(value.ExecutionID, message)
	case harnesssession.AgentEventType:
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.Apply(value.ExecutionID, agentEvent)
	case harnesssession.ExecutionStartedEntryType:
		m.executionID = value.ExecutionID
		m.notice = notice{}
		m.transcript.Start(value.ExecutionID)
	case harnesssession.ExecutionFinishedEntryType:
		var finished harnesssession.ExecutionFinished
		if err := json.Unmarshal(value.Payload, &finished); err != nil {
			m.notice = errorNotice(err)

			return
		}

		if value.ExecutionID == m.executionID {
			m.executionID = ""
		}

		m.transcript.Finish(value.ExecutionID)
		m.notice = executionNotice(finished)
	}
}
