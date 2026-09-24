package session

import (
	"context"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func (s *Session) checkpoint(
	ctx context.Context,
	executionID string,
	agentMemory *memory.Memory,
) error {
	previous, messages, err := agentMemory.Current(ctx)
	if err != nil {
		return fmt.Errorf("read checkpoint context: %w", err)
	}

	if err := s.Emit(ctx, executionID, checkpoint.StartedEventType, nil); err != nil {
		return err
	}

	generated, err := s.checkpoints.Generate(ctx, previous, messages)
	if err != nil {
		return err
	}

	if err := s.Commit(ctx, executionID, checkpoint.EntryType, generated); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}

	return nil
}
