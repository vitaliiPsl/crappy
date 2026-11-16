package background

import (
	"errors"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type State string

const (
	Running   State = "running"
	Completed State = "completed"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

var (
	ErrClosed   = errors.New("background jobs are stopped")
	ErrNotFound = errors.New("background job not found")
)

type Record struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	State     State  `json:"state"`
}

type Result struct {
	Record
	Output kit.ToolOutput
	Error  string
}
