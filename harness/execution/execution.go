package execution

import (
	"encoding/json"
	"time"
)

const (
	StartedEntryType  = "execution.started"
	FinishedEntryType = "execution.finished"
)

type Status string

const (
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

type Request struct {
	Executor string `json:"executor"`
	Input    any    `json:"input"`
}

type Started struct {
	Executor  string          `json:"executor"`
	Input     json.RawMessage `json:"input"`
	CreatedAt time.Time       `json:"created_at"`
	StartedAt time.Time       `json:"started_at"`
}

type Result struct {
	Outcome Status          `json:"outcome"`
	Output  json.RawMessage `json:"output,omitempty"`
	Error   string          `json:"error,omitempty"`
	EndedAt time.Time       `json:"ended_at"`
}
