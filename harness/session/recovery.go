package session

import (
	"context"
	"time"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func (s *Session) restore(ctx context.Context) error {
	id, err := s.unfinishedExecution(ctx)
	if err != nil || id == "" {
		return err
	}

	return s.Commit(ctx, id, execution.FinishedEntryType, execution.Result{
		Outcome: execution.Interrupted,
		EndedAt: time.Now().UTC(),
	})
}

func (s *Session) unfinishedExecution(ctx context.Context) (string, error) {
	started, found, err := s.entries.Latest(ctx, s.id, execution.StartedEntryType)
	if err != nil || !found {
		return "", err
	}

	finished, found, err := s.entries.Latest(ctx, s.id, execution.FinishedEntryType)
	if err != nil {
		return "", err
	}

	if found && finished.ExecutionID == started.ExecutionID && finished.ID > started.ID {
		return "", nil
	}

	return started.ExecutionID, nil
}
