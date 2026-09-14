package entry

import (
	"encoding/json"
	"time"
)

type Entry struct {
	ID          uint64 `json:"id"`
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id,omitempty"`

	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`

	CreatedAt time.Time `json:"created_at"`
}
