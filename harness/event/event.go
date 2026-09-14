package event

import (
	"context"
	"encoding/json"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type Publisher interface {
	Emit(ctx context.Context, executionID, eventType string, payload any) error
	Commit(ctx context.Context, executionID, entryType string, payloads ...any) error
}

type Event struct {
	EntryID     uint64 `json:"entry_id,omitempty"`
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id,omitempty"`

	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func FromEntry(value entry.Entry) Event {
	return Event{
		EntryID:     value.ID,
		SessionID:   value.SessionID,
		ExecutionID: value.ExecutionID,

		Type:    value.Type,
		Payload: append(json.RawMessage(nil), value.Payload...),
	}
}
