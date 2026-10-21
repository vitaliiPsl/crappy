package execution

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const (
	AgentEventType    = "agent.event"
	StartedEntryType  = "execution.started"
	FinishedEntryType = "execution.finished"
)

type Outcome string

const (
	Completed   Outcome = "completed"
	Failed      Outcome = "failed"
	Cancelled   Outcome = "cancelled"
	Interrupted Outcome = "interrupted"
)

type Started struct {
	Input kit.Message `json:"input"`
}

type Finished struct {
	Outcome   Outcome   `json:"outcome"`
	Error     string    `json:"error,omitempty"`
	Usage     kit.Usage `json:"usage"`
	LastUsage kit.Usage `json:"last_usage"`
}

type Execution struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
}

func (e *Execution) ID() string {
	return e.id
}

func (e *Execution) Cancel() {
	e.cancel()
}

func (e *Execution) Done() <-chan struct{} {
	return e.done
}
