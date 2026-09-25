package jsonl

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/vitaliipsl/crappy/harness/session"
)

const stateFilename = "session.json"

var _ session.Store = (*sessionStore)(nil)

type sessionStore struct {
	storage *Storage
}

func (s *sessionStore) Get(ctx context.Context, sessionID string) (session.State, error) {
	if err := ctx.Err(); err != nil {
		return session.State{}, err
	}

	state, err := s.storage.state(sessionID)
	if err != nil {
		return session.State{}, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	return s.read(sessionID)
}

func (s *sessionStore) Save(ctx context.Context, value session.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	state, err := s.storage.state(value.ID)
	if err != nil {
		return err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	directory, err := s.storage.createSessionDirectory(value.ID)
	if err != nil {
		return err
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}

	return writeFileAtomic(directory, stateFilename, data)
}

func (s *sessionStore) List(ctx context.Context) ([]session.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	directories, err := os.ReadDir(s.storage.root)
	if err != nil {
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	states := make([]session.State, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !directory.IsDir() || validateSessionID(directory.Name()) != nil {
			continue
		}

		value, err := s.Get(ctx, directory.Name())
		if errors.Is(err, session.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		states = append(states, value)
	}

	slices.SortFunc(states, func(a, b session.State) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})

	return states, nil
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

func (s *sessionStore) read(sessionID string) (session.State, error) {
	directory, err := s.storage.sessionDirectory(sessionID)
	if err != nil {
		return session.State{}, err
	}

	info, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return session.State{}, session.ErrNotFound
	}

	if err != nil {
		return session.State{}, fmt.Errorf("stat session directory: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, stateFilename))
	if errors.Is(err, os.ErrNotExist) {
		return session.State{ID: sessionID, CreatedAt: info.ModTime(), UpdatedAt: info.ModTime()}, nil
	}

	if err != nil {
		return session.State{}, fmt.Errorf("read session state: %w", err)
	}

	var value session.State
	if err := json.Unmarshal(data, &value); err != nil {
		return session.State{}, fmt.Errorf("decode session state: %w", err)
	}

	return value, nil
}

func writeFileAtomic(directory, name string, data []byte) (resultErr error) {
	file, err := os.CreateTemp(directory, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}

	defer func() {
		if resultErr != nil {
			_ = os.Remove(file.Name())
		}
	}()

	if _, err := file.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", name, err), file.Close())
	}

	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync %s: %w", name, err), file.Close())
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}

	if err := os.Rename(file.Name(), filepath.Join(directory, name)); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}

	return syncDirectory(directory)
}
