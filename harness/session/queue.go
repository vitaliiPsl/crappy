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
		s.current.cancel()

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

func (s *Session) startNext() {
	if s.ctx.Err() != nil || s.current != nil || len(s.queue) == 0 {
		return
	}

	next := s.queue[0]
	if err := s.commitExecution(s.ctx, next, execution.Running, ""); err != nil {
		s.halt(err)

		return
	}

	ctx, cancel := context.WithCancel(s.ctx)
	next.cancel = cancel
	s.queue = slices.Delete(s.queue, 0, 1)
	s.current = next

	go s.runExecution(ctx, next)
}
