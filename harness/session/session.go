package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var ErrBusy = errors.New("session is busy")

type State struct {
	ID string
}

type Session struct {
	state   State
	entries entry.Store
	events  *event.Bus
	agents  *agent.Factory

	execution sync.Mutex
}

func New(state State, entries entry.Store, agents *agent.Factory) *Session {
	events := event.NewBus(state.ID, entries)

	return &Session{
		state:   state,
		entries: entries,
		events:  events,
		agents:  agents,
	}
}

func (s *Session) ID() string {
	return s.state.ID
}

func (s *Session) Entries(ctx context.Context, afterID uint64) ([]entry.Entry, error) {
	return s.entries.List(ctx, s.state.ID, afterID)
}

func (s *Session) Subscribe(ctx context.Context, afterID uint64) (*event.Subscription, error) {
	return s.events.Subscribe(ctx, afterID)
}

func (s *Session) Emit(value event.Event) {
	s.events.Emit(value)
}

func (s *Session) Execute(ctx context.Context, input kit.Message) (kit.AgentResponse, error) {
	if !s.execution.TryLock() {
		return kit.AgentResponse{}, ErrBusy
	}
	defer s.execution.Unlock()

	executionID := uuid.NewString()
	agentMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	agent, err := s.agents.Build(agentMemory)
	if err != nil {
		return kit.AgentResponse{}, fmt.Errorf("build agent: %w", err)
	}

	stream := agent.Stream(ctx, input)
	for agentEvent := range stream.Iter() {
		value, err := newAgentEvent(executionID, agentEvent)
		if err != nil {
			return kit.AgentResponse{}, err
		}

		s.Emit(value)
	}

	return stream.Result()
}

func newAgentEvent(executionID string, value kit.AgentEvent) (event.Event, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return event.Event{}, fmt.Errorf("encode agent event: %w", err)
	}

	return event.Event{
		ExecutionID: executionID,
		Type:        "agent." + string(value.Type),
		Payload:     payload,
	}, nil
}
