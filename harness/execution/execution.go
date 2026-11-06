package execution

import "encoding/json"

const EntryType = "execution"

type Status string

const (
	Queued      Status = "queued"
	Running     Status = "running"
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

func (s Status) Terminal() bool {
	switch s {
	case Completed, Failed, Cancelled:
		return true
	default:
		return false
	}
}

type Request struct {
	Executor string `json:"executor"`
	Input    any    `json:"input"`
}

func NewRequest(executor string, input any) Request {
	return Request{Executor: executor, Input: input}
}

type Record struct {
	ID       string          `json:"id"`
	Executor string          `json:"executor"`
	Input    json.RawMessage `json:"input"`
	Status   Status          `json:"status"`
	Error    string          `json:"error,omitempty"`
}
