package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

type Session struct {
	ctx   context.Context
	state State

	entries  entry.Store
	events   *event.Bus
	executor *execution.Executor

	mu     sync.Mutex
	active *execution.Execution
	closed bool
}

func New(
	ctx context.Context,
	state State,
	entries entry.Store,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) (*Session, error) {
	events := event.NewBus(state.ID, entries)

	session := &Session{
		ctx:      ctx,
		state:    state,
		entries:  entries,
		events:   events,
		executor: execution.NewExecutor(state.ID, entries, events, agents, checkpoints),
	}

	if err := session.executor.Reconcile(ctx); err != nil {
		return nil, fmt.Errorf("reconcile executions: %w", err)
	}

	return session, nil
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

func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	active := s.active
	s.mu.Unlock()

	if active != nil {
		active.Cancel()
		<-active.Done()
	}

	s.events.Close()
}
