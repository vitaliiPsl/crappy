package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var ErrBusy = errors.New("session is busy")

type State struct {
	ID string
}

type Session struct {
	state   State
	entries entry.Store
	agents  *agent.Factory

	execution sync.Mutex
}

func New(state State, entries entry.Store, agents *agent.Factory) *Session {
	return &Session{
		state:   state,
		entries: entries,
		agents:  agents,
	}
}

func (s *Session) ID() string {
	return s.state.ID
}

func (s *Session) Entries(ctx context.Context, afterID uint64) ([]entry.Entry, error) {
	return s.entries.List(ctx, s.state.ID, afterID)
}

func (s *Session) Execute(ctx context.Context, input kit.Message) (kit.AgentResponse, error) {
	if !s.execution.TryLock() {
		return kit.AgentResponse{}, ErrBusy
	}
	defer s.execution.Unlock()

	executionID := uuid.NewString()
	agentMemory := memory.New(s.state.ID, executionID, s.entries)

	agent, err := s.agents.Build(agentMemory)
	if err != nil {
		return kit.AgentResponse{}, fmt.Errorf("build agent: %w", err)
	}

	return agent.Run(ctx, input)
}
