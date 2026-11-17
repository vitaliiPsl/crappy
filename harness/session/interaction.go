package session

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/interaction"
)

func (s *Session) Ask(ctx context.Context, executionID string, question interaction.Question) (interaction.Answer, error) {
	return s.interactions.Ask(ctx, executionID, question)
}

func (s *Session) Respond(ctx context.Context, requestID string, answer interaction.Answer) error {
	return s.interactions.Respond(ctx, requestID, answer)
}

func (s *Session) PendingPrompts(ctx context.Context) ([]interaction.Request, error) {
	return s.interactions.Pending(ctx)
}
