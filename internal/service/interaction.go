package service

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/interaction"
)

func (s *Local) PendingPrompts(ctx context.Context, sessionID string) ([]interaction.Request, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.PendingPrompts(ctx)
}

func (s *Local) Respond(ctx context.Context, sessionID, requestID string, answer interaction.Answer) error {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return err
	}

	return opened.Respond(ctx, requestID, answer)
}
