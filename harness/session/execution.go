package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/execution"
)

var (
	ErrBusy                = errors.New("session is busy")
	ErrClosed              = errors.New("session is closed")
	ErrExecutionNotActive  = errors.New("execution is not active")
	ErrExecutionNotFound   = errors.New("execution not found")
	ErrExecutionUnfinished = errors.New("execution is unfinished")
)

type Execution struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}

	status execution.Status
	err    error
}

func (e *Execution) ID() string {
	return e.id
}

func (e *Execution) Status() (execution.Status, error) {
	select {
	case <-e.done:
		if e.err != nil {
			return "", e.err
		}

		return e.status, nil
	default:
		return execution.Running, nil
	}
}

func (e *Execution) Done() <-chan struct{} {
	return e.done
}

func (e *Execution) Wait(ctx context.Context) (execution.Status, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-e.done:
	}

	if e.err != nil {
		return "", e.err
	}

	return e.status, nil
}

func (s *Session) Execute(ctx context.Context, request execution.Request) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.closed || s.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if s.current != nil {
		return nil, ErrBusy
	}

	executor, err := s.executors.Resolve(request.Executor)
	if err != nil {
		return nil, err
	}

	input, err := json.Marshal(request.Input)
	if err != nil {
		return nil, fmt.Errorf("encode execution input: %w", err)
	}

	id := uuid.NewString()

	if err := s.Commit(ctx, id, execution.StartedEntryType, execution.Started{
		Executor:  request.Executor,
		Input:     input,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		return nil, fmt.Errorf("start execution: %w", err)
	}

	runCtx, cancel := context.WithCancel(s.ctx)
	current := &Execution{
		id:     id,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.current = current

	go s.runExecution(runCtx, executor, current, input)

	return current, nil
}

func (s *Session) Execution(ctx context.Context, id string) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.current != nil && s.current.ID() == id {
		return s.current, nil
	}

	entries, err := s.entries.List(ctx, s.id, 0)
	if err != nil {
		return nil, err
	}

	var started bool

	for _, value := range entries {
		if value.ExecutionID != id {
			continue
		}

		if value.Type == execution.StartedEntryType {
			started = true
		}

		if value.Type != execution.FinishedEntryType {
			continue
		}

		var finished execution.Finished
		if err := json.Unmarshal(value.Payload, &finished); err != nil {
			return nil, fmt.Errorf("decode execution %s finish: %w", id, err)
		}

		loaded := &Execution{
			id:     id,
			done:   make(chan struct{}),
			status: finished.Status,
		}
		close(loaded.done)

		return loaded, nil
	}

	if !started {
		return nil, ErrExecutionNotFound
	}

	return nil, ErrExecutionUnfinished
}

func (s *Session) Cancel(ctx context.Context, executionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrClosed
	}

	if s.current == nil || s.current.ID() != executionID {
		return ErrExecutionNotActive
	}

	s.current.cancel()

	return nil
}

func (s *Session) runExecution(ctx context.Context, executor execution.Executor, current *Execution, input json.RawMessage) {
	defer current.cancel()

	runErr := executor.Execute(ctx, execution.ExecuteParams{
		SessionID:   s.id,
		ExecutionID: current.ID(),
		Input:       input,
		State:       s.state,
		Entries:     s.entries,
		Events:      s.events,
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	runErr = errors.Join(runErr, ctx.Err())
	finished := execution.Finished{Status: execution.Completed, EndedAt: time.Now().UTC()}

	switch {
	case errors.Is(runErr, context.Canceled):
		finished.Status = execution.Cancelled
	case runErr != nil:
		finished.Status = execution.Failed
		finished.Error = runErr.Error()
	}

	s.finishExecution(context.WithoutCancel(ctx), finished)
}

func (s *Session) finishExecution(ctx context.Context, finished execution.Finished) {
	current := s.current

	err := s.Commit(ctx, current.ID(), execution.FinishedEntryType, finished)
	if err != nil {
		err = fmt.Errorf("finish execution %s: %w", current.ID(), err)
	}

	if err != nil {
		s.closed = true
		s.events.Close()
	} else {
		s.current = nil
	}

	current.status = finished.Status
	current.err = err

	close(current.done)
}
