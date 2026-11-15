package service

import (
	"context"
	"fmt"
	"os"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/utils"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func (s *Local) CreateSession(ctx context.Context, directory string) (state.Record, error) {
	if directory == "" {
		var err error

		directory, err = os.Getwd()
		if err != nil {
			return state.Record{}, fmt.Errorf("get process workdir: %w", err)
		}
	}

	directory, err := utils.ResolvePath(directory)
	if err != nil {
		return state.Record{}, err
	}

	opened, err := s.harness.CreateSession(ctx, map[string]any{workdir.StateKey: directory})
	if err != nil {
		return state.Record{}, err
	}

	return s.sessions.Get(ctx, opened.ID())
}

func (s *Local) GetSession(ctx context.Context, sessionID string) (state.Record, error) {
	if sessionID == "" {
		return state.Record{}, harness.ErrSessionRequired
	}

	return s.sessions.Get(ctx, sessionID)
}

func (s *Local) ListSessions(ctx context.Context) ([]state.Record, error) {
	return s.sessions.List(ctx)
}

func (s *Local) DeleteSession(ctx context.Context, sessionID string) error {
	return s.harness.DeleteSession(ctx, sessionID)
}
