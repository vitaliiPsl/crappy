package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var (
	ErrBusy               = errors.New("session is busy")
	ErrExecutionNotActive = errors.New("execution is not active")
)

const (
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

type ExecutionStarted struct{}

type ExecutionFinished struct {
	Outcome   ExecutionOutcome `json:"outcome"`
	Error     string           `json:"error,omitempty"`
	Usage     kit.Usage        `json:"usage"`
	LastUsage kit.Usage        `json:"last_usage"`
}

type execution struct {
	id     string
	cancel context.CancelFunc
}

func (s *Session) Execute(input kit.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
		return "", ErrBusy
	}

	executionID := uuid.NewString()
	agentMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	agent, err := s.agents.Build(context.Background(), agent.BuildRequest{
		SessionID:   s.state.ID,
		ExecutionID: executionID,
		Memory:      agentMemory,
		Checkpoint: func(ctx context.Context) error {
			return s.checkpoint(ctx, executionID)
		},
	})
	if err != nil {
		return "", fmt.Errorf("build agent: %w", err)
	}

	executionContext, cancel := context.WithCancel(context.Background())
	running := &execution{
		id:     executionID,
		cancel: cancel,
	}

	if err := s.Commit(
		context.Background(),
		executionID,
		ExecutionStartedEntryType,
		ExecutionStarted{},
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
		defer s.mu.Unlock()

		if s.active == execution {
			s.active = nil
		}
	}()

	stream := agent.Stream(ctx, input)

	var runErr error
	for agentEvent := range stream.Iter() {
		eventType := "agent." + string(agentEvent.Type)
		if err := s.Emit(ctx, execution.id, eventType, agentEvent); err != nil {
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
