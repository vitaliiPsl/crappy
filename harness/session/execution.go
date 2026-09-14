package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/memory"
)

var ErrBusy = errors.New("session is busy")

var ErrExecutionNotActive = errors.New("execution is not active")

const AgentResultEntryType = "agent.result"

type AgentResult struct {
	Usage     kit.Usage `json:"usage"`
	LastUsage kit.Usage `json:"last_usage"`
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

	builtAgent, err := s.agents.Build(agentMemory)
	if err != nil {
		return "", fmt.Errorf("build agent: %w", err)
	}

	executionContext, cancel := context.WithCancel(context.Background())
	execution := &execution{id: executionID, cancel: cancel}
	s.active = execution

	go s.runExecution(executionContext, execution, builtAgent, input)

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
	builtAgent kit.Agent,
	input kit.Message,
) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.active == execution {
			s.active = nil
		}
	}()

	stream := builtAgent.Stream(ctx, input)
	for agentEvent := range stream.Iter() {
		eventType := "agent." + string(agentEvent.Type)
		if err := s.Emit(ctx, execution.id, eventType, agentEvent); err != nil {
			return
		}
	}

	response, err := stream.Result()
	if err != nil {
		return
	}

	_ = s.Commit(context.WithoutCancel(ctx), execution.id, AgentResultEntryType, AgentResult{
		Usage:     response.Usage,
		LastUsage: response.LastUsage,
	})
}
