package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var (
	ErrBusy               = errors.New("session is busy")
	ErrClosed             = errors.New("session is closed")
	ErrExecutionNotActive = errors.New("execution is not active")
)

const (
	AgentEventType             = "agent.event"
	ExecutionStartedEntryType  = "execution.started"
	ExecutionFinishedEntryType = "execution.finished"
)

type ExecutionOutcome string

const (
	ExecutionCompleted   ExecutionOutcome = "completed"
	ExecutionFailed      ExecutionOutcome = "failed"
	ExecutionCancelled   ExecutionOutcome = "cancelled"
	ExecutionInterrupted ExecutionOutcome = "interrupted"
)

type ExecutionStarted struct {
	Input kit.Message `json:"input"`
}

type ExecutionFinished struct {
	Outcome   ExecutionOutcome `json:"outcome"`
	Error     string           `json:"error,omitempty"`
	Usage     kit.Usage        `json:"usage"`
	LastUsage kit.Usage        `json:"last_usage"`
}

type execution struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Session) Execute(input kit.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return "", ErrClosed
	}

	if s.active != nil {
		return "", ErrBusy
	}

	if err := s.reconcileExecutions(s.ctx); err != nil {
		return "", fmt.Errorf("reconcile executions: %w", err)
	}

	executionID := uuid.NewString()
	agentMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	agent, err := s.agents.Build(s.ctx, agent.BuildRequest{
		SessionID:   s.state.ID,
		ExecutionID: executionID,
		Memory:      agentMemory,
		Checkpoint: func(ctx context.Context) (*checkpoint.Checkpoint, error) {
			return s.checkpoint(ctx, executionID, agentMemory)
		},
	})
	if err != nil {
		return "", fmt.Errorf("build agent: %w", err)
	}

	executionContext, cancel := context.WithCancel(s.ctx)
	running := &execution{
		id:     executionID,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	if err := s.Commit(
		s.ctx,
		executionID,
		ExecutionStartedEntryType,
		ExecutionStarted{Input: input},
	); err != nil {
		cancel()

		return "", fmt.Errorf("start execution: %w", err)
	}

	s.active = running

	go s.runExecution(executionContext, running, agent, input)

	return executionID, nil
}

func (s *Session) Cancel(executionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active == nil || s.active.id != executionID {
		return ErrExecutionNotActive
	}

	s.active.cancel()

	return nil
}

func (s *Session) runExecution(
	ctx context.Context,
	execution *execution,
	agent kit.Agent,
	input kit.Message,
) {
	defer func() {
		s.mu.Lock()
		if s.active == execution {
			s.active = nil
		}
		s.mu.Unlock()

		close(execution.done)
	}()

	stream := agent.Stream(ctx, input)

	var runErr error
	for agentEvent := range stream.Iter() {
		if err := s.Emit(ctx, execution.id, AgentEventType, agentEvent); err != nil {
			runErr = err

			break
		}
	}

	response, streamErr := stream.Result()
	if runErr == nil {
		runErr = streamErr
	}

	if runErr != nil {
		if err := s.reconcileTools(context.WithoutCancel(ctx), execution.id); err != nil {
			runErr = fmt.Errorf("reconcile tools: %w", err)
		}
	}

	finished := ExecutionFinished{
		Outcome:   ExecutionCompleted,
		Usage:     response.Usage,
		LastUsage: response.LastUsage,
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			finished.Outcome = ExecutionCancelled
		} else {
			finished.Outcome = ExecutionFailed
			finished.Error = runErr.Error()
		}
	}

	_ = s.Commit(
		context.WithoutCancel(ctx),
		execution.id,
		ExecutionFinishedEntryType,
		finished,
	)
}
