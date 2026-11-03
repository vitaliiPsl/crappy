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

func (a *Assistant) Execute(ctx context.Context, sessionID string, content ...kit.Content) (*session.Execution, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, kit.NewUserMessage(content...)))
}

func (a *Assistant) Execution(ctx context.Context, sessionID, executionID string) (*session.Execution, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Execution(ctx, executionID)
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

func (a *Assistant) Compact(ctx context.Context, sessionID string) (*session.Execution, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Execute(ctx, execution.NewRequest(compactionexec.ExecutorName, nil))
}
