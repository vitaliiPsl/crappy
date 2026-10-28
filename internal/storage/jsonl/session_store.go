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

	"github.com/vitaliipsl/crappy/harness/session/state"
)

const stateFilename = "session.json"

var _ state.Store = (*sessionStore)(nil)

type sessionStore struct {
	storage *Storage
}

func (s *sessionStore) Get(ctx context.Context, sessionID string) (state.Record, error) {
	if err := ctx.Err(); err != nil {
		return state.Record{}, err
	}

	current, err := s.storage.state(sessionID)
	if err != nil {
		return state.Record{}, err
	}

	current.mu.Lock()
	defer current.mu.Unlock()

	return s.read(sessionID)
}

func (s *sessionStore) Save(ctx context.Context, record state.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	current, err := s.storage.state(record.ID)
	if err != nil {
		return err
	}

	current.mu.Lock()
	defer current.mu.Unlock()

	directory, err := s.storage.createSessionDirectory(record.ID)
	if err != nil {
		return err
	}

	return s.write(ctx, directory, record)
}

func (s *sessionStore) Update(ctx context.Context, sessionID string, update func(*state.Record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	current, err := s.storage.state(sessionID)
	if err != nil {
		return err
	}

	current.mu.Lock()
	defer current.mu.Unlock()

	record, err := s.read(sessionID)
	if err != nil {
		return err
	}

	if err := update(&record); err != nil {
		return err
	}

	directory, err := s.storage.sessionDirectory(sessionID)
	if err != nil {
		return err
	}

	return s.write(ctx, directory, record)
}

func (s *sessionStore) List(ctx context.Context) ([]state.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	directories, err := os.ReadDir(s.storage.root)
	if err != nil {
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	states := make([]state.Record, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !directory.IsDir() || validateSessionID(directory.Name()) != nil {
			continue
		}

		record, err := s.Get(ctx, directory.Name())
		if errors.Is(err, state.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		states = append(states, record)
	}

	slices.SortFunc(states, func(a, b state.Record) int {
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

	current := s.storage.states[sessionID]
	if current == nil {
		current = &sessionState{}
	}

	current.mu.Lock()
	defer current.mu.Unlock()

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

func (s *sessionStore) read(sessionID string) (state.Record, error) {
	directory, err := s.storage.sessionDirectory(sessionID)
	if err != nil {
		return state.Record{}, err
	}

	info, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return state.Record{}, state.ErrNotFound
	}

	if err != nil {
		return state.Record{}, fmt.Errorf("stat session directory: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, stateFilename))
	if errors.Is(err, os.ErrNotExist) {
		return state.Record{ID: sessionID, CreatedAt: info.ModTime(), UpdatedAt: info.ModTime()}, nil
	}

	if err != nil {
		return state.Record{}, fmt.Errorf("read session state: %w", err)
	}

	var record *state.Record
	if err := json.Unmarshal(data, &record); err != nil {
		return state.Record{}, fmt.Errorf("decode session state: %w", err)
	}

	if record == nil {
		return state.Record{}, errors.New("session state must be a JSON object")
	}

	return *record, nil
}

func (s *sessionStore) write(ctx context.Context, directory string, record state.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return writeFileAtomic(directory, stateFilename, data)
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
