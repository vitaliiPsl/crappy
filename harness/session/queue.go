package session

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func (s *Session) Execute(ctx context.Context, request execution.Request) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if _, err := s.executors.Resolve(request.Executor); err != nil {
		return nil, err
	}

	input, err := json.Marshal(request.Input)
	if err != nil {
		return nil, fmt.Errorf("encode execution input: %w", err)
	}

	queued := newExecution(execution.Record{ID: uuid.NewString(), Executor: request.Executor, Input: input})
	if err := s.commitExecution(ctx, queued, execution.Queued, ""); err != nil {
		return nil, err
	}

	s.queue = append(s.queue, queued)
	s.startNext()

	return queued, nil
}

func (s *Session) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.ctx.Err() != nil {
		return ErrClosed
	}

	if s.current != nil && s.current.ID() == id {
		if s.current.record.Status == execution.Running {
			s.current.cancel()

			return nil
		}

		if err := s.commitExecution(ctx, s.current, execution.Cancelled, ""); err != nil {
			return err
		}

		s.current.stop(nil)
		s.current = nil
		s.startNext()

		return nil
	}

	for i, queued := range s.queue {
		if queued.ID() != id {
			continue
		}

		if err := s.commitExecution(ctx, queued, execution.Cancelled, ""); err != nil {
			return err
		}

		queued.stop(nil)

		s.queue = slices.Delete(s.queue, i, i+1)

		return nil
	}

	return ErrExecutionNotActive
}

func (s *Session) Resume(ctx context.Context) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if s.current == nil || s.current.record.Status == execution.Running {
		return nil, ErrExecutionNotResumable
	}

	if _, err := s.executors.Resolve(s.current.record.Executor); err != nil {
		return nil, err
	}

	current := s.current
	if err := s.startExecution(ctx, current); err != nil {
		return nil, err
	}

	return current, nil
}

func (s *Session) startNext() {
	if s.ctx.Err() != nil || s.current != nil || len(s.queue) == 0 {
		return
	}

	next := s.queue[0]
	if next.record.Status == execution.Interrupted {
		s.current = next
		s.queue = slices.Delete(s.queue, 0, 1)

		return
	}

	if err := s.startExecution(s.ctx, next); err != nil {
		s.halt(err)

		return
	}

	s.queue = slices.Delete(s.queue, 0, 1)
}

func (s *Session) startExecution(ctx context.Context, current *Execution) error {
	if err := s.commitExecution(ctx, current, execution.Running, ""); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(s.ctx)
	current.cancel = cancel
	s.current = current

	go s.runExecution(ctx, current)

	return nil
}
