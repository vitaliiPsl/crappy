package schedule

import (
	"context"
	"fmt"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Dispatcher interface {
	Dispatch(context.Context, Record) (string, error)
}

type ExecutionDispatcher struct {
	harness *harness.Harness
}

func NewExecutionDispatcher(harness *harness.Harness) *ExecutionDispatcher {
	return &ExecutionDispatcher{harness: harness}
}

func (d *ExecutionDispatcher) Dispatch(ctx context.Context, record Record) (string, error) {
	opened, err := d.harness.Session(ctx, record.SessionID)
	if err != nil {
		return "", err
	}

	running, err := unfinished(ctx, opened, record.ExecutionID)
	if err != nil {
		return "", err
	}

	if running {
		return record.ExecutionID, nil
	}

	message := kit.NewUserMessage(kit.NewTextContent(requestText(record)))

	current, err := opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, message))
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}

func unfinished(ctx context.Context, opened *session.Session, executionID string) (bool, error) {
	if executionID == "" {
		return false, nil
	}

	previous, err := opened.Execution(ctx, executionID)
	if err != nil {
		return false, err
	}

	status, err := previous.Status()
	if err != nil {
		return false, err
	}

	return !status.Terminal(), nil
}

func requestText(record Record) string {
	due := record.At.Format(time.RFC3339Nano)
	if record.Cron == "" {
		return fmt.Sprintf("Scheduled request %s, due %s. Carry out this instruction:\n\n%s",
			record.ID, due, record.Instruction,
		)
	}

	return fmt.Sprintf("Recurring scheduled request %s (cron %q), due %s. "+
		"To stop the recurrence when the user asks or the instruction says it should end, use schedule_cancel with this ID. "+
		"Carry out this instruction:\n\n%s",
		record.ID, record.Cron, due, record.Instruction,
	)
}
