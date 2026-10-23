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
	ErrBusy               = errors.New("session is busy")
	ErrClosed             = errors.New("session is closed")
	ErrExecutionNotActive = errors.New("execution is not active")
)

func (s *Session) Execute(ctx context.Context, request execution.Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if s.closed || s.ctx.Err() != nil {
		return "", ErrClosed
	}

	if request.Executor == "" {
		request.Executor = execution.AgentName
	}

	executor, err := s.executors.Resolve(request.Executor)
	if err != nil {
		return "", err
	}

	input, err := json.Marshal(request.Input)
	if err != nil {
		return "", fmt.Errorf("encode execution input: %w", err)
	}

	if err := s.recoverExecution(ctx); err != nil {
		return "", err
	}

	executionID := uuid.NewString()

	now := time.Now().UTC()
	if err := s.Commit(ctx, executionID, execution.StartedEntryType, execution.Started{
		Executor:  request.Executor,
		Input:     input,
		CreatedAt: now,
		StartedAt: now,
	}); err != nil {
		return "", fmt.Errorf("start execution: %w", err)
	}

	runCtx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})

	s.activeID, s.cancel, s.done = executionID, cancel, done

	go func() {
		defer close(done)
		defer cancel()

		s.runExecution(runCtx, executor, executionID, input)
	}()

	return executionID, nil
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

	if s.activeID == "" || s.activeID != executionID || s.pending != nil {
		return ErrExecutionNotActive
	}

	s.cancel()

	return nil
}

func (s *Session) runExecution(ctx context.Context, configured execution.Executor, executionID string, input json.RawMessage) {
	runErr := configured.Execute(ctx, execution.ExecuteParams{
		SessionID:   s.id,
		ExecutionID: executionID,
		Input:       input,
		Entries:     s.entries,
		Events:      s.events,
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		runErr = errors.Join(runErr, ctx.Err())
	}

	s.pending = &execution.Finished{Outcome: execution.Completed, EndedAt: time.Now().UTC()}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			s.pending.Outcome = execution.Cancelled
		} else {
			s.pending.Outcome = execution.Failed
			s.pending.Error = runErr.Error()
		}
	}

	_ = s.finishExecution(context.WithoutCancel(ctx))
}

func (s *Session) finishExecution(ctx context.Context) error {
	if err := s.Commit(ctx, s.activeID, execution.FinishedEntryType, s.pending); err != nil {
		return fmt.Errorf("finish execution %s: %w", s.activeID, err)
	}

	s.activeID, s.pending = "", nil

	return nil
}

func (s *Session) recoverExecution(ctx context.Context) error {
	if s.activeID != "" {
		if s.pending == nil {
			return ErrBusy
		}

		return s.finishExecution(ctx)
	}

	started, found, err := s.entries.Latest(ctx, s.id, execution.StartedEntryType)
	if err != nil || !found {
		return err
	}

	finished, found, err := s.entries.Latest(ctx, s.id, execution.FinishedEntryType)
	if err != nil {
		return err
	}

	if found && finished.ExecutionID == started.ExecutionID && finished.ID > started.ID {
		return nil
	}

	s.activeID = started.ExecutionID
	s.pending = &execution.Finished{
		Outcome: execution.Interrupted,
		EndedAt: time.Now().UTC(),
	}

	return s.finishExecution(ctx)
}
