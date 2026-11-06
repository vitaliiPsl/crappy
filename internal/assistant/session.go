package assistant

import (
	"context"
	"fmt"
	"os"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/utils"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func (a *Assistant) GetSession(ctx context.Context, sessionID string) (state.Record, error) {
	if sessionID == "" {
		return state.Record{}, harness.ErrSessionRequired
	}

	return a.sessions.Get(ctx, sessionID)
}

func (a *Assistant) ListSessions(ctx context.Context) ([]state.Record, error) {
	return a.sessions.List(ctx)
}

func (a *Assistant) CreateSession(ctx context.Context, directory string) (state.Record, error) {
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

	opened, err := a.harness.CreateSession(ctx, map[string]any{workdir.StateKey: directory})
	if err != nil {
		return state.Record{}, err
	}

	return a.sessions.Get(ctx, opened.ID())
}

func (a *Assistant) DeleteSession(ctx context.Context, sessionID string) error {
	return a.harness.DeleteSession(ctx, sessionID)
}

func (a *Assistant) Subscribe(ctx context.Context, sessionID string, afterID uint64) (event.Subscription, error) {
	opened, err := a.harness.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Subscribe(ctx, afterID)
}
