package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session"
)

type CreateOptions struct {
	Title string
}

type Runtime struct {
	ctx context.Context

	sessions    session.Store
	entries     entry.Store
	agents      *agent.Factory
	checkpoints *checkpoint.Generator

	mu     sync.Mutex
	opened map[string]*session.Session
}

func New(
	ctx context.Context,
	sessions session.Store,
	entries entry.Store,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) *Runtime {
	return &Runtime{
		ctx:         ctx,
		sessions:    sessions,
		agents:      agents,
		entries:     entries,
		checkpoints: checkpoints,
		opened:      make(map[string]*session.Session),
	}
}

func (r *Runtime) Create(ctx context.Context, options CreateOptions) (session.State, error) {
	now := time.Now().UTC()
	state := session.State{
		ID:        uuid.NewString(),
		Title:     options.Title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := r.sessions.Save(ctx, state); err != nil {
		return session.State{}, fmt.Errorf("save session: %w", err)
	}

	return state, nil
}

func (r *Runtime) List(ctx context.Context) ([]session.State, error) {
	return r.sessions.List(ctx)
}

func (r *Runtime) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if opened := r.opened[sessionID]; opened != nil {
		opened.Close()
		delete(r.opened, sessionID)
	}

	return r.sessions.Delete(ctx, sessionID)
}

func (r *Runtime) Open(ctx context.Context, sessionID string) (*session.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing := r.opened[sessionID]; existing != nil {
		return existing, nil
	}

	state, err := r.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	opened, err := session.New(
		r.ctx,
		state,
		r.entries,
		r.agents,
		r.checkpoints,
	)
	if err != nil {
		return nil, err
	}

	r.opened[sessionID] = opened

	return opened, nil
}

func (r *Runtime) Subscribe(
	ctx context.Context,
	sessionID string,
	afterEntryID uint64,
) (*event.Subscription, error) {
	opened, err := r.Open(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Subscribe(ctx, afterEntryID)
}

func (r *Runtime) Execute(
	ctx context.Context,
	sessionID string,
	input kit.Message,
) (string, error) {
	opened, err := r.Open(ctx, sessionID)
	if err != nil {
		return "", err
	}

	return opened.Execute(input)
}

func (r *Runtime) Cancel(
	ctx context.Context,
	sessionID string,
	executionID string,
) error {
	opened, err := r.Open(ctx, sessionID)
	if err != nil {
		return err
	}

	return opened.Cancel(executionID)
}
