package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/vitaliipsl/crappy/harness"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

var ErrSessionRequired = errors.New("an existing session is required")

type Assistant struct {
	harness  *harness.Harness
	sessions state.Store
}

func New(harness *harness.Harness, sessions state.Store) *Assistant {
	return &Assistant{
		harness:  harness,
		sessions: sessions,
	}
}

func (a *Assistant) Session(ctx context.Context, sessionID string) (*session.Session, error) {
	if sessionID != "" {
		return a.harness.Session(ctx, sessionID)
	}

	directory, err := a.Workdir(ctx, "")
	if err != nil {
		return nil, err
	}

	opened, err := a.harness.Session(ctx, "")
	if err != nil {
		return nil, err
	}

	if err := opened.State().Set(ctx, workdir.StateKey, directory); err != nil {
		return nil, fmt.Errorf("save session workdir: %w", err)
	}

	return opened, nil
}

func (a *Assistant) Sessions(ctx context.Context) ([]state.Record, error) {
	return a.sessions.List(ctx)
}

func (a *Assistant) DeleteSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrSessionRequired
	}

	opened, err := a.harness.Session(ctx, sessionID)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}

	if err == nil {
		opened.Close()
	}

	return a.sessions.Delete(ctx, sessionID)
}

func (a *Assistant) Compact(ctx context.Context, sessionID string) (*session.Execution, error) {
	if sessionID == "" {
		return nil, ErrSessionRequired
	}

	opened, err := a.harness.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return opened.Execute(ctx, execution.Request{Executor: compactionexec.ExecutorName})
}

func (a *Assistant) Workdir(ctx context.Context, sessionID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if sessionID != "" {
		opened, err := a.harness.Session(ctx, sessionID)
		if err != nil {
			return "", err
		}

		var directory string

		found, err := opened.State().Get(ctx, workdir.StateKey, &directory)
		if err != nil {
			return "", fmt.Errorf("read session workdir: %w", err)
		}

		if found {
			return directory, nil
		}
	}

	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get process workdir: %w", err)
	}

	return directory, nil
}
