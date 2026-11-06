package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

var (
	ErrClosed          = errors.New("harness is closed")
	ErrAlreadyRunning  = errors.New("harness is already running")
	ErrSessionRequired = errors.New("an existing session is required")
)

type Store interface {
	SessionStore() state.Store
	EntryStore() entry.Store
}

type Option func(*Harness)

func WithExecutor(name string, executor execution.Executor) Option {
	return func(h *Harness) { h.executors.Register(name, executor) }
}

type Harness struct {
	sessions  state.Store
	entries   entry.Store
	executors *execution.Registry

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
		executors: execution.NewRegistry(),

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

func (h *Harness) CreateSession(ctx context.Context, values map[string]any) (*session.Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	record := state.Record{
		ID:        uuid.NewString(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(values) > 0 {
		record.Values = make(map[string]json.RawMessage, len(values))
	}

	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode session value %q: %w", key, err)
		}

		record.Values[key] = encoded
	}

	if err := h.sessions.Save(ctx, record); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}

	return h.openSession(ctx, record.ID)
}

func (h *Harness) Session(ctx context.Context, sessionID string) (*session.Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ctx.Err() != nil {
		return nil, ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if sessionID == "" {
		return nil, ErrSessionRequired
	}

	if opened := h.opened[sessionID]; opened != nil {
		return opened, nil
	}

	if _, err := h.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}

	return h.openSession(ctx, sessionID)
}

func (h *Harness) DeleteSession(ctx context.Context, sessionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ctx.Err() != nil {
		return ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if sessionID == "" {
		return ErrSessionRequired
	}

	// Keep the runtime cached while closing so lookups cannot construct a replacement.
	if opened := h.opened[sessionID]; opened != nil {
		h.mu.Unlock()
		opened.Close()
		h.mu.Lock()
	}

	if err := h.sessions.Delete(ctx, sessionID); err != nil {
		return err
	}

	delete(h.opened, sessionID)

	return nil
}

func (h *Harness) openSession(ctx context.Context, sessionID string) (*session.Session, error) {
	events := event.NewBroker(sessionID, h.entries)

	opened, err := session.New(h.ctx, sessionID, state.New(h.sessions, sessionID), h.entries, events, h.executors)
	if err != nil {
		if h.ctx.Err() != nil {
			return nil, ErrClosed
		}

		return nil, fmt.Errorf("open session %s: %w", sessionID, err)
	}

	if h.ctx.Err() != nil {
		opened.Close()

		return nil, ErrClosed
	}

	h.opened[sessionID] = opened

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return opened, nil
}

func (h *Harness) closeSessions() {
	h.mu.Lock()

	opened := make([]*session.Session, 0, len(h.opened))
	for _, s := range h.opened {
		opened = append(opened, s)
	}
	h.mu.Unlock()

	for _, s := range opened {
		s.Close()
	}

	close(h.done)
}
