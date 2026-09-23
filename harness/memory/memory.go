package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

const EntryType = "message"

type entryMemory struct {
	sessionID   string
	executionID string
	entries     entry.Store
	events      event.Publisher
}

func New(sessionID, executionID string, entries entry.Store, events event.Publisher) kit.Memory {
	return &entryMemory{
		sessionID:   sessionID,
		executionID: executionID,
		entries:     entries,
		events:      events,
	}
}

func (m *entryMemory) Context(ctx context.Context) ([]kit.Message, error) {
	entries, err := m.entries.List(ctx, m.sessionID, 0)
	if err != nil {
		return nil, err
	}

	latest, messages, err := AfterCheckpoint(entries)
	if err != nil {
		return nil, err
	}

	context := make([]kit.Message, 0, len(messages)+1)
	if latest != nil {
		context = append(context, latest.Message())
	}

	return append(context, messages...), nil
}

func (m *entryMemory) History(ctx context.Context) ([]kit.Message, error) {
	entries, err := m.entries.List(ctx, m.sessionID, 0)
	if err != nil {
		return nil, err
	}

	return messages(entries)
}

func (m *entryMemory) Record(ctx context.Context, messages ...kit.Message) error {
	payloads := make([]any, len(messages))
	for i, message := range messages {
		payloads[i] = message
	}

	return m.events.Commit(ctx, m.executionID, EntryType, payloads...)
}

func AfterCheckpoint(entries []entry.Entry) (*checkpoint.Checkpoint, []kit.Message, error) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != checkpoint.EntryType {
			continue
		}

		var latest checkpoint.Checkpoint
		if err := json.Unmarshal(entries[i].Payload, &latest); err != nil {
			return nil, nil, fmt.Errorf("decode checkpoint entry %d: %w", entries[i].ID, err)
		}

		messages, err := messages(entries[retainedFrom(entries[:i], latest.Retained):])
		if err != nil {
			return nil, nil, err
		}

		return &latest, messages, nil
	}

	messages, err := messages(entries)

	return nil, messages, err
}

func retainedFrom(entries []entry.Entry, retained int) int {
	start := len(entries)
	for start > 0 && retained > 0 {
		start--
		if entries[start].Type == EntryType {
			retained--
		}
	}

	return start
}

func messages(entries []entry.Entry) ([]kit.Message, error) {
	messages := make([]kit.Message, 0, len(entries))
	for _, value := range entries {
		if value.Type != EntryType {
			continue
		}

		message, err := decode(value)
		if err != nil {
			return nil, err
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func decode(value entry.Entry) (kit.Message, error) {
	var message kit.Message
	if err := json.Unmarshal(value.Payload, &message); err != nil {
		return kit.Message{}, fmt.Errorf("decode message entry %d: %w", value.ID, err)
	}

	return message, nil
}
