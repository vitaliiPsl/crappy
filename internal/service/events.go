package service

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/event"
)

func (s *Local) Subscribe(ctx context.Context, sessionID string, afterID uint64) (event.Subscription, error) {
	opened, err := s.harness.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Subscribe(ctx, afterID)
}
