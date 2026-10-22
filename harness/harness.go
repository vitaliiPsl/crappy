package harness

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
)

var (
	ErrClosed         = errors.New("harness is closed")
	ErrAlreadyRunning = errors.New("harness is already running")
)

type Store interface {
	SessionStore() session.Store
	EntryStore() entry.Store
}

type Option func(*Harness)

func WithExecutors(executors map[string]execution.Executor) Option {
	return func(h *Harness) {
		maps.Copy(h.executors, executors)
	}
}

type Harness struct {
	sessions  session.Store
	entries   entry.Store
	executors map[string]execution.Executor

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	opened  map[string]*session.Session
	stop    sync.Once
	running bool
}

func New(store Store, options ...Option) *Harness {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Harness{
		sessions:  store.SessionStore(),
		entries:   store.EntryStore(),
		executors: make(map[string]execution.Executor),

		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		opened: make(map[string]*session.Session),
	}
	for _, option := range options {
		option(h)
	}

	return h
}

func (h *Harness) Session(ctx context.Context, sessionID string) (*session.Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if h.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if opened := h.opened[sessionID]; opened != nil {
		return opened, nil
	}

	var state session.State
	if sessionID == "" {
		now := time.Now().UTC()

		state = session.State{
			ID:        uuid.NewString(),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.sessions.Save(ctx, state); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
	} else {
		var err error

		state, err = h.sessions.Get(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}

	opened := session.New(
		h.ctx,
		state.ID,
		h.entries,
		session.WithExecutors(h.executors),
	)

	h.opened[state.ID] = opened

	return opened, nil
}

func (h *Harness) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()

		return ErrClosed
	}

	if h.running {
		h.mu.Unlock()

		return ErrAlreadyRunning
	}

	h.running = true
	h.mu.Unlock()

	select {
	case <-ctx.Done():
		_ = h.Shutdown(context.Background())

		return ctx.Err()
	case <-h.done:
		return nil
	}
}

func (h *Harness) Shutdown(ctx context.Context) error {
	h.stop.Do(func() {
		h.cancel()

		go h.closeSessions()
	})

	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Harness) closeSessions() {
	h.mu.Lock()
	opened := h.opened
	h.mu.Unlock()

	for _, s := range opened {
		s.Close()
	}

	close(h.done)
}
