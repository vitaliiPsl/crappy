package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func (s *Session) checkpoint(
	ctx context.Context,
	executionID string,
) error {
	sessionMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	messages, err := sessionMemory.Context(ctx)
	if err != nil {
		return fmt.Errorf("read checkpoint context: %w", err)
	}

	if len(messages) == 0 {
		return errors.New("no context to checkpoint")
	}

	generated, err := s.checkpoints.Generate(ctx, messages)
	if err != nil {
		return err
	}

	if err := s.Commit(ctx, executionID, checkpoint.EntryType, generated); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}

	return nil
}
