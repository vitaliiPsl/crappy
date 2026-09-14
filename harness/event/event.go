package event

import (
	"encoding/json"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type Emitter interface {
	Emit(Event)
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
