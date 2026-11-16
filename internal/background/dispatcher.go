package background

import (
	"context"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
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

	text := fmt.Sprintf("Background job %s (%s) is %s. Use job_get to read its result and continue the work.",
		record.ID, record.Tool, record.State,
	)
	message := kit.NewUserMessage(kit.NewTextContent(text))

	return opened.Steer(ctx, execution.NewRequest(agent.ExecutorName, message))
}
