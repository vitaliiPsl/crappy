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
	events      event.Emitter
}

func New(sessionID, executionID string, entries entry.Store, events event.Emitter) kit.Memory {
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
	entries := make([]entry.Entry, len(messages))
	for i, message := range messages {
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode message: %w", err)
		}

		entries[i] = entry.Entry{
			SessionID:   m.sessionID,
			ExecutionID: m.executionID,
			Type:        messageEntryType,
			Payload:     payload,
		}
	}

	appended, err := m.entries.Append(ctx, entries...)
	if err != nil {
		return err
	}

	for _, value := range appended {
		m.events.Emit(event.FromEntry(value))
	}

	return nil
}

func hasSummary(message kit.Message) bool {
	for _, content := range message.Content {
		if content.Type == kit.ContentTypeSummary && content.Summary != nil {
			return true
		}
	}

	return false
}
