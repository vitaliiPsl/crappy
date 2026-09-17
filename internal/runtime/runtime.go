package runtime

import (
	"context"
	"sync"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session"
)

type Runtime struct {
	entries     entry.Store
	agents      *agent.Factory
	checkpoints *checkpoint.Generator

	mu       sync.Mutex
	sessions map[string]*session.Session
}

func NewRuntime(
	entries entry.Store,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) *Runtime {
	return &Runtime{
		agents:      agents,
		entries:     entries,
		checkpoints: checkpoints,
		sessions:    make(map[string]*session.Session),
	}
}

func (r *Runtime) Session(ctx context.Context, sessionID string) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing := r.sessions[sessionID]; existing != nil {
		return existing, nil
	}

	state := session.State{ID: sessionID}

	opened, err := session.New(
		ctx,
		state,
		r.entries,
		r.agents,
		r.checkpoints,
	)
	if err != nil {
		return nil, err
	}

	r.sessions[sessionID] = opened

	return opened, nil
}

func (r *Runtime) Execute(
	ctx context.Context,
	sessionID string,
	input kit.Message,
) (string, error) {
	opened, err := r.Session(ctx, sessionID)
	if err != nil {
		return "", err
	}

	return opened.Execute(input)
}
