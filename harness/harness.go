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
	"github.com/vitaliipsl/crappy/harness/session/state"
)

var (
	ErrClosed         = errors.New("harness is closed")
	ErrAlreadyRunning = errors.New("harness is already running")
)

type Store interface {
	SessionStore() state.Store
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
	sessions  state.Store
	entries   entry.Store
	executors execution.Resolver

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	opened  map[string]*session.Session
	pending map[string]*sessionOpening
	opening sync.WaitGroup
	stop    sync.Once
	running bool
}

type sessionOpening struct {
	done    chan struct{}
	session *session.Session
	err     error
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

		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		opened:  make(map[string]*session.Session),
		pending: make(map[string]*sessionOpening),
	}
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

func (h *Harness) Session(ctx context.Context, sessionID string) (*session.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if h.ctx.Err() != nil {
		return nil, ErrClosed
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()

	record, err := h.sessionState(ctx, sessionID)
	if err != nil {
		if h.ctx.Err() != nil {
			return nil, ErrClosed
		}

		if errors.Is(err, state.ErrNotFound) {
			h.mu.Lock()
			opened := h.opened[sessionID]
			delete(h.opened, sessionID)
			h.mu.Unlock()

			if opened != nil {
				opened.Close()
			}
		}

		return nil, err
	}

	return h.openSession(ctx, record.ID)
}

func (h *Harness) openSession(ctx context.Context, sessionID string) (*session.Session, error) {
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()

		return nil, ErrClosed
	}

	if err := ctx.Err(); err != nil {
		h.mu.Unlock()

		return nil, err
	}

	if opened := h.opened[sessionID]; opened != nil {
		h.mu.Unlock()

		return opened, nil
	}

	pending := h.pending[sessionID]
	if pending == nil {
		pending = &sessionOpening{done: make(chan struct{})}
		h.pending[sessionID] = pending
		h.opening.Add(1)

		go h.constructSession(sessionID, pending)
	}
	h.mu.Unlock()

	select {
	case <-ctx.Done():
		if h.ctx.Err() != nil {
			return nil, ErrClosed
		}

		return nil, ctx.Err()
	case <-pending.done:
	}

	return pending.session, pending.err
}

func (h *Harness) constructSession(sessionID string, pending *sessionOpening) {
	defer h.opening.Done()

	opened, err := session.New(h.ctx, sessionID, state.New(h.sessions, sessionID), h.entries, h.executors)
	if err != nil {
		err = fmt.Errorf("open session %s: %w", sessionID, err)
	}

	h.mu.Lock()
	if h.ctx.Err() != nil {
		err = ErrClosed
	}

	if err == nil {
		h.opened[sessionID] = opened
		pending.session = opened
	}

	pending.err = err
	delete(h.pending, sessionID)
	h.mu.Unlock()

	if err != nil && opened != nil {
		opened.Close()
	}

	close(pending.done)
}

func (h *Harness) sessionState(ctx context.Context, sessionID string) (state.Record, error) {
	if sessionID != "" {
		return h.sessions.Get(ctx, sessionID)
	}

	now := time.Now().UTC()

	record := state.Record{
		ID:        uuid.NewString(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.sessions.Save(ctx, record); err != nil {
		return state.Record{}, fmt.Errorf("save session: %w", err)
	}

	return record, nil
}

func (h *Harness) closeSessions() {
	h.mu.Lock()

	opened := make([]*session.Session, 0, len(h.opened))
	for _, s := range h.opened {
		opened = append(opened, s)
	}
	h.mu.Unlock()

	h.opening.Wait()

	for _, s := range opened {
		s.Close()
	}

	close(h.done)
}
