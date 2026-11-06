package session

import (
	"encoding/json"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/assistant"
)

func (m *Model) applyEvent(value event.Event) {
	usage, err := assistant.UsageFromEvent(value)
	if err != nil {
		m.notice = errorNotice(err)

		return
	}

	m.sessionUsage.Add(usage)

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
	case execution.EntryType:
		var record execution.Record
		if err := json.Unmarshal(value.Payload, &record); err != nil {
			m.notice = errorNotice(err)

			return
		}

		record.ID = value.ExecutionID
		m.applyExecution(record)
	}
}
