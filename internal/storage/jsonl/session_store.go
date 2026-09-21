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

func (s *sessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.storage.sessionDirectory(sessionID)
	if err != nil {
		return err
	}

	s.storage.mu.Lock()
	defer s.storage.mu.Unlock()

	state := s.storage.states[sessionID]
	if state == nil {
		state = &sessionState{}
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove session directory: %w", err)
	}

	delete(s.storage.states, sessionID)

	if err := syncDirectory(s.storage.root); err != nil {
		return fmt.Errorf("sync sessions directory: %w", err)
	}

	return nil
}
