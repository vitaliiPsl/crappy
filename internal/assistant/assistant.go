package assistant

import (
	"context"
	"fmt"
	"os"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

type Assistant struct {
	harness *harness.Harness
}

func New(harness *harness.Harness) *Assistant {
	return &Assistant{
		harness: harness,
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
