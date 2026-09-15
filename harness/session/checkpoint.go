package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var ErrNoCheckpointContext = errors.New("session has no context to checkpoint")

func (s *Session) Checkpoint(ctx context.Context) (*checkpoint.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
		return nil, ErrBusy
	}

	return s.checkpoint(ctx, "")
}

func (s *Session) checkpointCallback(executionID string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := s.checkpoint(ctx, executionID)

		return err
	}
}

func (s *Session) checkpoint(
	ctx context.Context,
	executionID string,
) (*checkpoint.Checkpoint, error) {
	sessionMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	messages, err := sessionMemory.Context(ctx)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint context: %w", err)
	}

	if len(messages) == 0 {
		return nil, ErrNoCheckpointContext
	}

	generated, err := s.checkpoints.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	if err := s.Commit(ctx, executionID, checkpoint.EntryType, generated); err != nil {
		return nil, fmt.Errorf("commit checkpoint: %w", err)
	}

	return generated, nil
}
