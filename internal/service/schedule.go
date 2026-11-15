package service

import (
	"context"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/internal/schedule"
)

func (s *Local) CreateSchedule(ctx context.Context, sessionID, instruction string, at time.Time) (schedule.Record, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return schedule.Record{}, err
	}

	return s.schedules.Create(ctx, sessionID, instruction, at)
}

func (s *Local) GetSchedule(ctx context.Context, sessionID, id string) (schedule.Record, error) {
	return s.schedules.Get(ctx, sessionID, id)
}

func (s *Local) ListSchedules(ctx context.Context, sessionID string) ([]schedule.Record, error) {
	if sessionID == "" {
		return nil, harness.ErrSessionRequired
	}

	return s.schedules.List(ctx, sessionID)
}

func (s *Local) CancelSchedule(ctx context.Context, sessionID, id string) (schedule.Record, error) {
	return s.schedules.Cancel(ctx, sessionID, id)
}
