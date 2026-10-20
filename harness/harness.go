package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
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

type Config struct {
	Store       Store
	Agent       *agent.Factory
	Checkpoints *checkpoint.Generator
}

type Harness struct {
	sessions    session.Store
	entries     entry.Store
	agents      *agent.Factory
	checkpoints *checkpoint.Generator

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	opened  map[string]*session.Session
	stop    sync.Once
	running bool
}

func New(config Config) (*Harness, error) {
	if config.Store == nil {
		return nil, errors.New("store is required")
	}

	sessions, entries := config.Store.SessionStore(), config.Store.EntryStore()
	if sessions == nil || entries == nil {
		return nil, errors.New("session and entry stores are required")
	}

	if config.Agent == nil {
		return nil, errors.New("agent factory is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Harness{
		sessions:    sessions,
		entries:     entries,
		agents:      config.Agent,
		checkpoints: config.Checkpoints,

		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		opened: make(map[string]*session.Session),
	}, nil
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

	opened, err := session.New(h.ctx, state, h.entries, h.agents, h.checkpoints)
	if err != nil {
		return nil, err
	}

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
