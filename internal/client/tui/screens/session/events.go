package session

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/internal/service"
)

func (m *Model) applyEvent(value event.Event) tea.Cmd {
	usage, err := service.UsageFromEvent(value)
	if err != nil {
		m.notice = errorNotice(err)

		return nil
	}

	m.sessionUsage.Add(usage)

	switch value.Type {
	case interaction.RequestedEntryType, interaction.AnsweredEntryType, interaction.CancelledEntryType:
		return m.applyPromptEvent(value)
	case agent.MessageEntryType:
		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		m.transcript.Append(value.ExecutionID, message)
	case agent.EventType:
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		m.transcript.Apply(value.ExecutionID, agentEvent)
	case compaction.StartedEventType:
		m.transcript.StartCompaction(value.ExecutionID)
	case compaction.EntryType:
		var generated compaction.Compaction
		if err := json.Unmarshal(value.Payload, &generated); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		m.transcript.AppendCompaction(value.ExecutionID, generated)
	case execution.EntryType:
		var record execution.Record
		if err := json.Unmarshal(value.Payload, &record); err != nil {
			m.notice = errorNotice(err)

			return nil
		}

		record.ID = value.ExecutionID
		m.applyExecution(record)
	}

	return nil
}
