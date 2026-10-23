package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
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

type Option func(*config)

type config struct {
	models       agent.ModelResolver
	agentConfig  kit.AgentConfig
	agentOptions []agent.Option
	executors    *execution.Registry
}

func WithModel(models agent.ModelResolver) Option {
	return func(c *config) { c.models = models }
}

func WithAgentConfig(configured kit.AgentConfig, options ...agent.Option) Option {
	return func(c *config) {
		c.agentConfig = configured
		c.agentOptions = append(c.agentOptions, options...)
	}
}

func WithExecutor(name string, executor execution.Executor) Option {
	return func(c *config) { c.executors.Register(name, executor) }
}

type Harness struct {
	sessions  session.Store
	entries   entry.Store
	executors execution.Resolver

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	opened  map[string]*session.Session
	stop    sync.Once
	running bool
}

func New(store Store, options ...Option) *Harness {
	c := config{executors: execution.NewRegistry()}
	for _, option := range options {
		option(&c)
	}

	if c.models != nil {
		if _, err := c.executors.Resolve(agent.ExecutorName); err != nil {
			c.executors.Register(agent.ExecutorName, agent.NewExecutor(
				agent.New(c.agentConfig, c.models, c.agentOptions...),
			))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Harness{
		sessions:  store.SessionStore(),
		entries:   store.EntryStore(),
		executors: c.executors,

		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		opened: make(map[string]*session.Session),
	}
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
			if errors.Is(err, session.ErrNotFound) {
				if opened := h.opened[sessionID]; opened != nil {
					opened.Close()
					delete(h.opened, sessionID)
				}
			}

			return nil, err
		}
	}

	if opened := h.opened[state.ID]; opened != nil {
		return opened, nil
	}

	opened := session.New(
		h.ctx,
		state.ID,
		h.entries,
		h.executors,
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
