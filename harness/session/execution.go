package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/vitaliipsl/crappy/harness/execution"
)

var (
	ErrClosed                = errors.New("session is closed")
	ErrExecutionNotActive    = errors.New("execution is not active")
	ErrExecutionNotFound     = errors.New("execution not found")
	ErrExecutionUnfinished   = errors.New("execution is unfinished")
	ErrExecutionNotResumable = errors.New("execution is not resumable")
)

type Execution struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	record execution.Record
	err    error
}

func newExecution(record execution.Record) *Execution {
	created := &Execution{record: record, done: make(chan struct{})}
	if record.Status.Terminal() {
		close(created.done)
	}

	return created
}

func (e *Execution) ID() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.record.ID
}

func (e *Execution) Status() (execution.Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.err != nil {
		return "", e.err
	}

	return e.record.Status, nil
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

	return e.Status()
}

func (e *Execution) stop(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	select {
	case <-e.done:
		return
	default:
		e.err = err
		close(e.done)
	}
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

	for _, queued := range s.queue {
		if queued.ID() == id {
			return queued, nil
		}
	}

	entries, err := s.entries.List(ctx, s.id, 0)
	if err != nil {
		return nil, err
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != execution.EntryType || entries[i].ExecutionID != id {
			continue
		}

		var record execution.Record
		if err := json.Unmarshal(entries[i].Payload, &record); err != nil {
			return nil, fmt.Errorf("decode execution %s: %w", id, err)
		}

		if !record.Status.Terminal() {
			return nil, ErrExecutionUnfinished
		}

		return newExecution(record), nil
	}

	return nil, ErrExecutionNotFound
}

func (s *Session) runExecution(ctx context.Context, current *Execution) {
	defer current.cancel()

	runErr := s.invokeExecutor(ctx, current.record)

	s.mu.Lock()
	defer s.mu.Unlock()

	status, failure := execution.Completed, ""

	switch {
	case runErr == nil:
	case s.ctx.Err() != nil:
		status = execution.Interrupted
	case ctx.Err() != nil || errors.Is(runErr, context.Canceled):
		status = execution.Cancelled
	default:
		status, failure = execution.Failed, runErr.Error()
	}

	if err := s.commitExecution(context.WithoutCancel(ctx), current, status, failure); err != nil {
		s.halt(err)

		return
	}

	current.stop(nil)
	s.current = nil
	s.startNext()
}

func (s *Session) invokeExecutor(ctx context.Context, record execution.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	executor, err := s.executors.Resolve(record.Executor)
	if err != nil {
		return err
	}

	return executor.Execute(ctx, execution.ExecuteParams{
		SessionID:   s.id,
		ExecutionID: record.ID,
		Input:       record.Input,
		State:       s.state,
		Entries:     s.entries,
		Events:      s.events,
	})
}

func (s *Session) commitExecution(ctx context.Context, current *Execution, status execution.Status, failure string) error {
	saved := current.record
	saved.Status, saved.Error = status, failure

	if err := s.Commit(ctx, saved.ID, execution.EntryType, saved); err != nil {
		return fmt.Errorf("commit execution %s: %w", saved.ID, err)
	}

	current.mu.Lock()
	current.record = saved
	current.mu.Unlock()

	return nil
}
