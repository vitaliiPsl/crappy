package session

import (
	"context"
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

type Session struct {
	ctx context.Context
	id  string

	entries   entry.Store
	events    *event.Bus
	executors execution.Resolver

	current *Execution

	mu     sync.Mutex
	closed bool
}

func New(ctx context.Context, id string, entries entry.Store, executors execution.Resolver) *Session {
	return &Session{
		ctx:       ctx,
		id:        id,
		entries:   entries,
		events:    event.NewBus(id, entries),
		executors: executors,
	}
}

func (s *Session) ID() string {
	return s.id
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
	s.closed = true
	current := s.current
	s.mu.Unlock()

	if current != nil {
		if current.cancel != nil {
			current.cancel()
		}

		<-current.done
	}

	s.events.Close()
}
