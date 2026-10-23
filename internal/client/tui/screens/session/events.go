package session

import (
	"encoding/json"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/memory"
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
	case agent.EventType:
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.Apply(value.ExecutionID, agentEvent)
	case checkpoint.StartedEventType:
		m.transcript.StartCheckpoint(value.ExecutionID)
	case checkpoint.EntryType:
		var generated checkpoint.Checkpoint
		if err := json.Unmarshal(value.Payload, &generated); err != nil {
			m.notice = errorNotice(err)

			return
		}

		m.transcript.AppendCheckpoint(value.ExecutionID, generated)
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
