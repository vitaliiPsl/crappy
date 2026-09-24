package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

const EntryType = "message"

var _ kit.Memory = (*Memory)(nil)

type Memory struct {
	sessionID   string
	executionID string
	entries     entry.Store
	events      event.Publisher
}

func New(sessionID, executionID string, entries entry.Store, events event.Publisher) *Memory {
	return &Memory{
		sessionID:   sessionID,
		executionID: executionID,
		entries:     entries,
		events:      events,
	}
}

func (m *Memory) Context(ctx context.Context) ([]kit.Message, error) {
	latest, messages, err := m.Current(ctx)
	if err != nil {
		return nil, err
	}

	if latest == nil {
		return messages, nil
	}

	return append([]kit.Message{latest.Message()}, messages...), nil
}

func (m *Memory) History(ctx context.Context) ([]kit.Message, error) {
	entries, err := m.entries.List(ctx, m.sessionID, 0)
	if err != nil {
		return nil, err
	}

	return messages(entries)
}

func (m *Memory) Record(ctx context.Context, messages ...kit.Message) error {
	payloads := make([]any, len(messages))
	for i, message := range messages {
		payloads[i] = message
	}

	return m.events.Commit(ctx, m.executionID, EntryType, payloads...)
}

func (m *Memory) Current(ctx context.Context) (*checkpoint.Checkpoint, []kit.Message, error) {
	stored, found, err := m.entries.Latest(ctx, m.sessionID, checkpoint.EntryType)
	if err != nil {
		return nil, nil, err
	}

	var (
		latest  *checkpoint.Checkpoint
		afterID uint64
	)

	if found {
		latest = &checkpoint.Checkpoint{}
		if err := json.Unmarshal(stored.Payload, latest); err != nil {
			return nil, nil, fmt.Errorf("decode checkpoint entry %d: %w", stored.ID, err)
		}

		afterID = stored.ID
	}

	after, err := m.entries.List(ctx, m.sessionID, afterID)
	if err != nil {
		return nil, nil, err
	}

	messages, err := messages(after)
	if err != nil {
		return nil, nil, err
	}

	if latest == nil {
		return nil, messages, nil
	}

	return latest, append(slices.Clone(latest.Retained), messages...), nil
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
