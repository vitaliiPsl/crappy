package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

const MessageEntryType = "message"

type History struct {
	sessionID   string
	executionID string
	entries     entry.Store
	events      event.Publisher
}

var _ kit.History = (*History)(nil)

func NewHistory(sessionID, executionID string, entries entry.Store, events event.Publisher) *History {
	return &History{
		sessionID:   sessionID,
		executionID: executionID,
		entries:     entries,
		events:      events,
	}
}

func (h *History) Messages(ctx context.Context) ([]kit.Message, error) {
	previous, messages, err := h.uncompacted(ctx)
	if err != nil {
		return nil, err
	}

	if previous != nil {
		messages = append([]kit.Message{previous.Message()}, messages...)
	}

	return messages, nil
}

func (h *History) Append(ctx context.Context, messages ...kit.Message) error {
	payloads := make([]any, len(messages))
	for i, message := range messages {
		payloads[i] = message
	}

	return h.events.Commit(ctx, h.executionID, MessageEntryType, payloads...)
}

// Compact reports false, without calling the compactor, when there is nothing to compact.
func (h *History) Compact(ctx context.Context, compactor *compaction.Compactor) (compaction.Compaction, bool, error) {
	previous, messages, err := h.uncompacted(ctx)
	if err != nil {
		return compaction.Compaction{}, false, fmt.Errorf("read history: %w", err)
	}

	if previous == nil && len(messages) == 0 {
		return compaction.Compaction{}, false, nil
	}

	if err := h.events.Emit(ctx, h.executionID, compaction.StartedEventType, nil); err != nil {
		return compaction.Compaction{}, false, fmt.Errorf("start compaction: %w", err)
	}

	generated, err := compactor.Compact(ctx, previous, messages)
	if err != nil {
		return generated, false, err
	}

	if err := h.events.Commit(ctx, h.executionID, compaction.EntryType, generated); err != nil {
		return generated, false, fmt.Errorf("commit compaction: %w", err)
	}

	return generated, true, nil
}

func (h *History) uncompacted(ctx context.Context) (*compaction.Compaction, []kit.Message, error) {
	latest, found, err := h.entries.Latest(ctx, h.sessionID, compaction.EntryType)
	if err != nil {
		return nil, nil, err
	}

	var (
		previous *compaction.Compaction
		messages []kit.Message
		afterID  uint64
	)

	if found {
		previous = &compaction.Compaction{}
		if err := json.Unmarshal(latest.Payload, previous); err != nil {
			return nil, nil, fmt.Errorf("decode compaction entry %d: %w", latest.ID, err)
		}

		messages = append(messages, previous.Retained...)
		afterID = latest.ID
	}

	records, err := h.entries.List(ctx, h.sessionID, afterID)
	if err != nil {
		return nil, nil, err
	}

	for _, record := range records {
		if record.Type != MessageEntryType {
			continue
		}

		var message kit.Message
		if err := json.Unmarshal(record.Payload, &message); err != nil {
			return nil, nil, fmt.Errorf("decode message entry %d: %w", record.ID, err)
		}

		messages = append(messages, message)
	}

	return previous, messages, nil
}
