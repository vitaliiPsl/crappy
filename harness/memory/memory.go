package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

const messageEntryType = "message"

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
	messages, err := m.History(ctx)
	if err != nil {
		return nil, err
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if hasSummary(messages[i]) {
			return messages[i:], nil
		}
	}

	return messages, nil
}

func (m *entryMemory) History(ctx context.Context) ([]kit.Message, error) {
	entries, err := m.entries.List(ctx, m.sessionID, 0)
	if err != nil {
		return nil, err
	}

	messages := make([]kit.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != messageEntryType {
			continue
		}

		var message kit.Message
		if err := json.Unmarshal(entry.Payload, &message); err != nil {
			return nil, fmt.Errorf("decode message entry %d: %w", entry.ID, err)
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func (m *entryMemory) Record(ctx context.Context, messages ...kit.Message) error {
	payloads := make([]any, len(messages))
	for i, message := range messages {
		payloads[i] = message
	}

	return m.events.Commit(ctx, m.executionID, messageEntryType, payloads...)
}

func hasSummary(message kit.Message) bool {
	for _, content := range message.Content {
		if content.Type == kit.ContentTypeSummary && content.Summary != nil {
			return true
		}
	}

	return false
}
