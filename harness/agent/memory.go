package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

const MessageEntryType = "message"

type Memory struct {
	sessionID   string
	executionID string
	entries     entry.Store
	events      event.Publisher
}

var _ kit.Memory = (*Memory)(nil)

func NewMemory(sessionID, executionID string, entries entry.Store, events event.Publisher) *Memory {
	return &Memory{
		sessionID:   sessionID,
		executionID: executionID,
		entries:     entries,
		events:      events,
	}
}

func (m *Memory) Context(ctx context.Context) ([]kit.Message, error) {
	latest, found, err := m.entries.Latest(ctx, m.sessionID, checkpoint.EntryType)
	if err != nil {
		return nil, err
	}

	var afterID uint64
	var base []entry.Entry
	if found {
		afterID = latest.ID
		base = append(base, latest)
	}

	tail, err := m.entries.List(ctx, m.sessionID, afterID)
	if err != nil {
		return nil, err
	}

	return projectMessages(append(base, tail...))
}

func (m *Memory) History(ctx context.Context) ([]kit.Message, error) {
	stored, err := m.entries.List(ctx, m.sessionID, 0)
	if err != nil {
		return nil, err
	}

	var messages []kit.Message
	for _, value := range stored {
		if value.Type != MessageEntryType {
			continue
		}
		message, err := decodeMessage(value)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (m *Memory) Record(ctx context.Context, messages ...kit.Message) error {
	payloads := make([]any, len(messages))
	for i, message := range messages {
		payloads[i] = message
	}

	return m.events.Commit(ctx, m.executionID, MessageEntryType, payloads...)
}

func projectMessages(entries []entry.Entry) ([]kit.Message, error) {
	var messages []kit.Message
	for _, value := range entries {
		switch value.Type {
		case MessageEntryType:
			message, err := decodeMessage(value)
			if err != nil {
				return nil, err
			}
			messages = append(messages, message)
		case checkpoint.EntryType:
			var compacted checkpoint.Checkpoint
			if err := json.Unmarshal(value.Payload, &compacted); err != nil {
				return nil, fmt.Errorf("decode compaction entry %d: %w", value.ID, err)
			}
			if strings.TrimSpace(compacted.Objective) == "" {
				return nil, fmt.Errorf("compaction entry %d has no objective", value.ID)
			}
			messages = append([]kit.Message{compacted.Message()}, compacted.Retained...)
		}
	}
	return messages, nil
}

func decodeMessage(value entry.Entry) (kit.Message, error) {
	var message kit.Message
	if err := json.Unmarshal(value.Payload, &message); err != nil {
		return kit.Message{}, fmt.Errorf("decode message entry %d: %w", value.ID, err)
	}
	return message, nil
}
