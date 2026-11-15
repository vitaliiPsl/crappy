package schedule

import (
	"context"
	"fmt"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Dispatcher interface {
	Dispatch(context.Context, Record) error
}

type ExecutionDispatcher struct {
	harness *harness.Harness
}

func NewExecutionDispatcher(harness *harness.Harness) *ExecutionDispatcher {
	return &ExecutionDispatcher{harness: harness}
}

func (d *ExecutionDispatcher) Dispatch(ctx context.Context, record Record) error {
	opened, err := d.harness.Session(ctx, record.SessionID)
	if err != nil {
		return err
	}

	instruction := fmt.Sprintf("Scheduled request %s, due %s. Carry out this instruction:\n\n%s",
		record.ID, record.At.Format(time.RFC3339Nano), record.Instruction,
	)
	message := kit.NewUserMessage(kit.NewTextContent(instruction))
	_, err = opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, message))

	return err
}
