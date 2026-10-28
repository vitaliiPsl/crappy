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

	directory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get process workdir: %w", err)
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
