package session

import (
	"context"
	"sync"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

type State struct {
	ID string
}

type Session struct {
	state       State
	entries     entry.Store
	events      *event.Bus
	agents      *agent.Factory
	checkpoints *checkpoint.Generator

	mu     sync.Mutex
	active *execution
}

func New(
	state State,
	entries entry.Store,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) *Session {
	events := event.NewBus(state.ID, entries)

	return &Session{
		state:       state,
		entries:     entries,
		events:      events,
		agents:      agents,
		checkpoints: checkpoints,
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

func (s *Session) Emit(
	ctx context.Context,
	executionID, eventType string,
	payload any,
) error {
	return s.events.Emit(ctx, executionID, eventType, payload)
}

func (s *Session) Commit(
	ctx context.Context,
	executionID, entryType string,
	payloads ...any,
) error {
	return s.events.Commit(ctx, executionID, entryType, payloads...)
}
