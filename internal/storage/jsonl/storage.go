package jsonl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session"
)

const transcriptFilename = "transcript.jsonl"

type Storage struct {
	root string

	mu     sync.Mutex
	states map[string]*sessionState
}

func Open(ctx context.Context, root string) (*Storage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if root == "" {
		return nil, fmt.Errorf("sessions path is required")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve sessions path: %w", err)
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create sessions directory: %w", err)
	}

	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("protect sessions directory: %w", err)
	}

	return &Storage{
		root:   root,
		states: make(map[string]*sessionState),
	}, nil
}

func (s *Storage) SessionStore() session.Store {
	return &sessionStore{storage: s}
}

func (s *Storage) EntryStore() entry.Store {
	return &entryStore{storage: s}
}

func (s *Storage) state(sessionID string) (*sessionState, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.states[sessionID]
	if state == nil {
		state = &sessionState{}
		s.states[sessionID] = state
	}

	return state, nil
}
