package schedule

import (
	"errors"
	"time"
)

type State string

const (
	Scheduled   State = "scheduled"
	Dispatching State = "dispatching"
	Dispatched  State = "dispatched"
	Cancelled   State = "cancelled"
	Failed      State = "failed"
)

var (
	ErrNotFound   = errors.New("schedule not found")
	ErrNotPending = errors.New("schedule is no longer pending")
	ErrInvalid    = errors.New("invalid schedule")
)

type Record struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Instruction string    `json:"instruction"`
	At          time.Time `json:"at"`
	State       State     `json:"state"`
}
