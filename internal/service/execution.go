package service

import (
	"context"
	"errors"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
)

func (s *Local) Execute(ctx context.Context, sessionID string, content ...kit.Content) (string, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, kit.NewUserMessage(content...)))
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}

func (s *Local) Cancel(ctx context.Context, sessionID, executionID string) error {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return err
	}

	if err := opened.Cancel(ctx, executionID); !errors.Is(err, session.ErrExecutionNotActive) {
		return err
	}

	return nil
}

func (s *Local) Resume(ctx context.Context, sessionID string) (string, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Resume(ctx)
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}

func (s *Local) Compact(ctx context.Context, sessionID string) (string, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execute(ctx, execution.NewRequest(compactionexec.ExecutorName, nil))
	if err != nil {
		return "", err
	}

	return current.ID(), nil
}

func (s *Local) GetExecutionStatus(ctx context.Context, sessionID, executionID string) (execution.Status, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execution(ctx, executionID)
	if err != nil {
		return "", err
	}

	return current.Status()
}

func (s *Local) WaitExecution(ctx context.Context, sessionID, executionID string) (execution.Status, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	current, err := opened.Execution(ctx, executionID)
	if err != nil {
		return "", err
	}

	return current.Wait(ctx)
}
