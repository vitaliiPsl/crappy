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
	Running     Status = "running"
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

type Request struct {
	Executor string `json:"executor"`
	Input    any    `json:"input"`
}

func NewRequest(executor string, input any) Request {
	return Request{Executor: executor, Input: input}
}

type Started struct {
	Executor  string          `json:"executor"`
	Input     json.RawMessage `json:"input"`
	StartedAt time.Time       `json:"started_at"`
}

type Finished struct {
	Status  Status    `json:"status"`
	Error   string    `json:"error,omitempty"`
	EndedAt time.Time `json:"ended_at"`
}
