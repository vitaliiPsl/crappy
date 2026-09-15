package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var (
	ErrCheckpointUnavailable = errors.New("checkpoint generator is not configured")
	ErrNoCheckpointContext   = errors.New("session has no context to checkpoint")
)

func (s *Session) Checkpoint(ctx context.Context) (*checkpoint.Checkpoint, error) {
	if s.checkpoints == nil {
		return nil, ErrCheckpointUnavailable
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
		return nil, ErrBusy
	}

	sessionMemory := memory.New(s.state.ID, "", s.entries, s.events)

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

	if err := s.Commit(ctx, "", checkpoint.EntryType, generated); err != nil {
		return nil, fmt.Errorf("commit checkpoint: %w", err)
	}

	return generated, nil
}
