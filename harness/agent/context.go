package agent

import (
	"context"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type ContextResolver struct {
	factory *Factory
}

func NewContextResolver(factory *Factory) *ContextResolver {
	return &ContextResolver{factory: factory}
}

func (r *ContextResolver) Resolve(
	ctx context.Context,
	sessionID string,
	state state.State,
	entries entry.Store,
	events event.Publisher,
) (kit.AgentContext, error) {
	built, err := r.factory.Build(ctx, BuildRequest{
		SessionID: sessionID,
		Memory:    NewMemory(sessionID, "", entries, events),
		State:     state,
		Entries:   entries,
		Events:    events,
	})
	if err != nil {
		return kit.AgentContext{}, fmt.Errorf("build agent: %w", err)
	}

	return built.Context(ctx)
}
