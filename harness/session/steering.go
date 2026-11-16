package session

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func (s *Session) Steer(ctx context.Context, request execution.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.ctx.Err() != nil {
		return ErrClosed
	}

	if s.current == nil || s.current.record.Status != execution.Running || s.current.record.Executor != request.Executor {
		_, err := s.enqueue(ctx, request)

		return err
	}

	s.steering = append(s.steering, request)

	return nil
}

func (s *Session) steer(ctx context.Context, accept func(json.RawMessage) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	pending := slices.Clone(s.steering)
	s.mu.Unlock()

	for _, request := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}

		input, err := json.Marshal(request.Input)
		if err != nil {
			return fmt.Errorf("encode steering input: %w", err)
		}

		if err := accept(input); err != nil {
			return err
		}

		s.mu.Lock()
		s.steering = slices.Delete(s.steering, 0, 1)
		s.mu.Unlock()
	}

	return nil
}

func (s *Session) enqueueRemainingSteering(ctx context.Context) error {
	for len(s.steering) > 0 {
		if _, err := s.enqueue(ctx, s.steering[0]); err != nil {
			return err
		}

		s.steering = slices.Delete(s.steering, 0, 1)
	}

	return nil
}
