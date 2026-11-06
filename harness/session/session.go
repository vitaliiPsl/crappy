package session

import (
	"context"
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type Session struct {
	ctx    context.Context
	cancel context.CancelFunc

	id string

	state     state.State
	entries   entry.Store
	events    *event.Bus
	executors execution.Resolver

	current *Execution
	queue   []*Execution

	mu sync.Mutex
}

func New(
	ctx context.Context,
	id string,
	state state.State,
	entries entry.Store,
	executors execution.Resolver,
) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		ctx:       ctx,
		cancel:    cancel,
		id:        id,
		state:     state,
		entries:   entries,
		events:    event.NewBus(id, entries),
		executors: executors,
	}

	if err := s.restore(ctx); err != nil {
		s.cancel()
		s.events.Close()

		return nil, err
	}

	return s, nil
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) State() state.State {
	return s.state
}

func (s *Session) Entries(ctx context.Context, afterID uint64) ([]entry.Entry, error) {
	return s.entries.List(ctx, s.id, afterID)
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
	s.cancel()

	current := s.current
	if current != nil && current.record.Status != execution.Running {
		current = nil
	}

	s.mu.Unlock()

	if current != nil {
		<-current.Done()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.halt(ErrClosed)
}

func (s *Session) halt(err error) {
	s.cancel()

	if s.current != nil {
		s.current.stop(err)
	}

	for _, queued := range s.queue {
		queued.stop(err)
	}

	s.events.Close()
}
