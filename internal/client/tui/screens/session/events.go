package session

import (
	"encoding/json"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func (m *Model) applyEvent(value event.Event) {
	switch value.Type {
	case agent.MessageEntryType:
		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.Append(value.ExecutionID, message)
	case agent.EventType:
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.Apply(value.ExecutionID, agentEvent)
	case compaction.StartedEventType:
		m.transcript.StartCompaction(value.ExecutionID)
	case compaction.EntryType:
		var generated compaction.Compaction
		if err := json.Unmarshal(value.Payload, &generated); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.AppendCompaction(value.ExecutionID, generated)
	case execution.StartedEntryType:
		m.executionID = value.ExecutionID
		m.notice = notice{}
		m.transcript.Start(value.ExecutionID)
	case execution.FinishedEntryType:
		var finished execution.Result
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
