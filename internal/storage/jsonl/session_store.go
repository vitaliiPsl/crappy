package jsonl

import (
	"context"
	"fmt"
	"os"

	"github.com/vitaliipsl/crappy/harness/session"
)

var _ session.Store = (*sessionStore)(nil)

type sessionStore struct {
	storage *Storage
}

func (s *sessionStore) List(ctx context.Context) ([]session.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	directories, err := os.ReadDir(s.storage.root)
	if err != nil {
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	sessions := make([]session.State, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !directory.IsDir() || validateSessionID(directory.Name()) != nil {
			continue
		}

		sessions = append(sessions, session.State{ID: directory.Name()})
	}

	return sessions, nil
}
