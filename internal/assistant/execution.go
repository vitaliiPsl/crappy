package assistant

import (
	"context"
	"errors"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
)

func (a *Assistant) Execute(ctx context.Context, sessionID string, content ...kit.Content) (string, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, kit.NewUserMessage(content...)))
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}

func (a *Assistant) GetExecutionStatus(ctx context.Context, sessionID, executionID string) (execution.Status, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execution(ctx, executionID)
	if err != nil {
		return "", err
	}

	return current.Status()
}

func (a *Assistant) WaitExecution(ctx context.Context, sessionID, executionID string) (execution.Status, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execution(ctx, executionID)
	if err != nil {
		return "", err
	}

	return current.Wait(ctx)
}

func (a *Assistant) Cancel(ctx context.Context, sessionID, executionID string) error {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return err
	}

	if err := opened.Cancel(ctx, executionID); !errors.Is(err, session.ErrExecutionNotActive) {
		return err
	}

	return nil
}

func (a *Assistant) Compact(ctx context.Context, sessionID string) (string, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execute(ctx, execution.NewRequest(compactionexec.ExecutorName, nil))
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}
