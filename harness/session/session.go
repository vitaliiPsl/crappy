package session

import (
	"context"
	"maps"
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
	executors map[string]execution.Executor

	activeID string
	cancel   context.CancelFunc
	done     chan struct{}
	pending  *execution.Finished

	mu     sync.Mutex
	closed bool
}

type Option func(*Session)

func WithExecutors(executors map[string]execution.Executor) Option {
	return func(s *Session) {
		maps.Copy(s.executors, executors)
	}
}

func New(ctx context.Context, id string, entries entry.Store, options ...Option) *Session {
	s := &Session{
		ctx:       ctx,
		id:        id,
		entries:   entries,
		events:    event.NewBus(id, entries),
		executors: make(map[string]execution.Executor),
	}
	for _, option := range options {
		option(s)
	}

	return s
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
	cancel, done := s.cancel, s.done
	s.mu.Unlock()

	if cancel != nil {
		cancel()
		<-done
	}

	s.events.Close()
}
