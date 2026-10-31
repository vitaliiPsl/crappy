package session

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func (s *Session) Context(ctx context.Context) (kit.AgentContext, error) {
	if err := ctx.Err(); err != nil {
		return kit.AgentContext{}, err
	}

	s.mu.Lock()
	closed := s.closed || s.ctx.Err() != nil
	s.mu.Unlock()

	if closed {
		return kit.AgentContext{}, ErrClosed
	}

	return s.contexts.Resolve(ctx, s.id, s.state, s.entries, s.events)
}
