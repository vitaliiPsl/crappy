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

	return contextMessages(entries)
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

func contextMessages(entries []entry.Entry) ([]kit.Message, error) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != checkpoint.EntryType {
			continue
		}

		var latest checkpoint.Checkpoint
		if err := json.Unmarshal(entries[i].Payload, &latest); err != nil {
			return nil, fmt.Errorf("decode checkpoint entry %d: %w", entries[i].ID, err)
		}

		projected, err := messages(entries[i+1:])
		if err != nil {
			return nil, err
		}

		return append(
			[]kit.Message{kit.NewUserMessage(kit.NewTextContent(latest.Summary))},
			projected...,
		), nil
	}

	return messages(entries)
}

func messages(entries []entry.Entry) ([]kit.Message, error) {
	messages := make([]kit.Message, 0, len(entries))
	for _, value := range entries {
		if value.Type != EntryType {
			continue
		}

		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			return nil, fmt.Errorf("decode message entry %d: %w", value.ID, err)
		}

		messages = append(messages, message)
	}

	return messages, nil
}
