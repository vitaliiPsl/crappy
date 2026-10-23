package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

	mu     sync.RWMutex
	result *execution.Result
	err    error
}

func (e *Execution) ID() string {
	return e.id
}

func (e *Execution) Done() <-chan struct{} {
	return e.done
}

func (e *Execution) Wait(ctx context.Context) (execution.Result, error) {
	if err := ctx.Err(); err != nil {
		return execution.Result{}, err
	}

	select {
	case <-ctx.Done():
		return execution.Result{}, ctx.Err()
	case <-e.done:
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.err != nil {
		return execution.Result{}, e.err
	}

	result := *e.result
	result.Output = bytes.Clone(result.Output)

	return result, nil
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

	if request.Executor == "" {
		request.Executor = execution.DefaultName
	}

	executor, err := s.executors.Resolve(request.Executor)
	if err != nil {
		return nil, err
	}

	input, err := json.Marshal(request.Input)
	if err != nil {
		return nil, fmt.Errorf("encode execution input: %w", err)
	}

	if s.current != nil {
		return nil, ErrBusy
	}

	unfinishedID, err := s.unfinishedExecution(ctx)
	if err != nil {
		return nil, err
	}

	if unfinishedID != "" {
		return nil, ErrExecutionUnfinished
	}

	id := uuid.NewString()
	now := time.Now().UTC()

	if err := s.Commit(ctx, id, execution.StartedEntryType, execution.Started{
		Executor:  request.Executor,
		Input:     input,
		CreatedAt: now,
		StartedAt: now,
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

	if s.current != nil && s.current.id == id {
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

		var result execution.Result
		if err := json.Unmarshal(value.Payload, &result); err != nil {
			return nil, fmt.Errorf("decode execution %s result: %w", id, err)
		}

		loaded := &Execution{id: id, done: make(chan struct{}), result: &result}
		close(loaded.done)

		return loaded, nil
	}

	if !started {
		return nil, ErrExecutionNotFound
	}

	return nil, ErrExecutionUnfinished
}

func (s *Session) Recover(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed || s.ctx.Err() != nil {
		return ErrClosed
	}

	if s.current != nil {
		if s.current.result == nil {
			return ErrBusy
		}

		return s.finishExecution(ctx, *s.current.result)
	}

	id, err := s.unfinishedExecution(ctx)
	if err != nil || id == "" {
		return err
	}

	s.current = &Execution{id: id, done: make(chan struct{})}

	return s.finishExecution(ctx, execution.Result{
		Outcome: execution.Interrupted,
		EndedAt: time.Now().UTC(),
	})
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

	if s.current == nil || s.current.id != executionID || s.current.result != nil {
		return ErrExecutionNotActive
	}

	s.current.cancel()

	return nil
}

func (s *Session) runExecution(ctx context.Context, executor execution.Executor, current *Execution, input json.RawMessage) {
	defer current.cancel()

	output, runErr := executor.Execute(ctx, execution.ExecuteParams{
		SessionID:   s.id,
		ExecutionID: current.id,
		Input:       input,
		Entries:     s.entries,
		Events:      s.events,
	})

	result := execution.Result{Outcome: execution.Completed}

	if output != nil {
		var err error

		result.Output, err = json.Marshal(output)
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("encode execution output: %w", err))
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	runErr = errors.Join(runErr, ctx.Err())
	result.EndedAt = time.Now().UTC()

	switch {
	case errors.Is(runErr, context.Canceled):
		result.Outcome = execution.Cancelled
	case runErr != nil:
		result.Outcome = execution.Failed
		result.Error = runErr.Error()
	}

	_ = s.finishExecution(context.WithoutCancel(ctx), result)
}

func (s *Session) finishExecution(ctx context.Context, result execution.Result) error {
	current := s.current

	err := s.Commit(ctx, current.id, execution.FinishedEntryType, result)
	if err != nil {
		err = fmt.Errorf("finish execution %s: %w", current.id, err)
	}

	current.mu.Lock()
	current.result = &result
	current.err = err
	current.mu.Unlock()

	if err == nil {
		s.current = nil
	}

	select {
	case <-current.done:
	default:
		close(current.done)
	}

	return err
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
