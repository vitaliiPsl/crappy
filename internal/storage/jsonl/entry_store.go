package jsonl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
)

var _ entry.Store = (*entryStore)(nil)

type entryStore struct {
	storage *Storage
}

func (s *entryStore) Append(
	ctx context.Context,
	sessionID string,
	entries ...entry.Entry,
) ([]entry.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return []entry.Entry{}, nil
	}

	state, err := s.storage.state(sessionID)
	if err != nil {
		return nil, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.append(ctx, sessionID, state, entries)
}

func (s *entryStore) List(
	ctx context.Context,
	sessionID string,
	afterID uint64,
) ([]entry.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	state, err := s.storage.state(sessionID)
	if err != nil {
		return nil, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	index, err := s.index(ctx, sessionID, state)
	if err != nil {
		return nil, err
	}

	entries := make([]entry.Entry, 0)

	err = s.scan(ctx, sessionID, index.offsetAfter(afterID), func(value entry.Entry, _ int64) bool {
		if value.ID > afterID {
			entries = append(entries, value)
		}

		return true
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *entryStore) Latest(
	ctx context.Context,
	sessionID, entryType string,
) (entry.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return entry.Entry{}, false, err
	}

	state, err := s.storage.state(sessionID)
	if err != nil {
		return entry.Entry{}, false, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	index, err := s.index(ctx, sessionID, state)
	if err != nil {
		return entry.Entry{}, false, err
	}

	latest, found := index.latest[entryType]
	if !found {
		return entry.Entry{}, false, nil
	}

	var value entry.Entry

	err = s.scan(ctx, sessionID, latest.offset, func(current entry.Entry, _ int64) bool {
		value = current

		return false
	})
	if err != nil {
		return entry.Entry{}, false, err
	}

	if value.ID != latest.id {
		return entry.Entry{}, false, fmt.Errorf("transcript index is stale for entry %d", latest.id)
	}

	return value, true, nil
}

func (s *entryStore) index(
	ctx context.Context,
	sessionID string,
	state *sessionState,
) (*transcriptIndex, error) {
	if state.index != nil {
		return state.index, nil
	}

	index := newTranscriptIndex()
	if err := s.scan(ctx, sessionID, 0, index.add); err != nil {
		return nil, err
	}

	state.index = index

	return index, nil
}

func (s *entryStore) scan(
	ctx context.Context,
	sessionID string,
	offset int64,
	visit func(entry.Entry, int64) bool,
) (resultErr error) {
	path := filepath.Join(s.storage.root, sessionID, transcriptFilename)

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}

	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()

	return scanEntries(ctx, file, offset, visit)
}

func (s *entryStore) append(
	ctx context.Context,
	sessionID string,
	state *sessionState,
	entries []entry.Entry,
) (_ []entry.Entry, resultErr error) {
	directory, err := s.storage.createSessionDirectory(sessionID)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(directory, transcriptFilename)
	_, statErr := os.Stat(path)

	newFile := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !newFile {
		return nil, fmt.Errorf("stat transcript: %w", statErr)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}

	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()

	if err := file.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("protect transcript: %w", err)
	}

	index, err := s.index(ctx, sessionID, state)
	if err != nil {
		return nil, err
	}

	prepared := prepareEntries(sessionID, entries, index.lastID)

	lines, err := encodeEntries(prepared)
	if err != nil {
		return nil, fmt.Errorf("encode transcript entries: %w", err)
	}

	encoded := bytes.Join(lines, nil)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat transcript: %w", err)
	}

	offset := info.Size()

	written, err := file.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}

	if err != nil {
		state.index = nil

		return nil, rollback(file, offset, fmt.Errorf("append transcript: %w", err))
	}

	if err := ctx.Err(); err != nil {
		state.index = nil

		return nil, rollback(file, offset, err)
	}

	if err := file.Sync(); err != nil {
		state.index = nil

		return nil, rollback(file, offset, fmt.Errorf("sync transcript: %w", err))
	}

	for i, value := range prepared {
		index.add(value, offset)
		offset += int64(len(lines[i]))
	}

	if newFile {
		if err := syncDirectory(directory); err != nil {
			return nil, fmt.Errorf("sync session directory: %w", err)
		}
	}

	return prepared, nil
}

func prepareEntries(
	sessionID string,
	entries []entry.Entry,
	lastID uint64,
) []entry.Entry {
	prepared := make([]entry.Entry, len(entries))
	nextID := lastID + 1

	createdAt := time.Now().UTC()
	for index, current := range entries {
		current.ID = nextID + uint64(index)
		current.SessionID = sessionID
		current.Payload = append([]byte(nil), current.Payload...)
		current.CreatedAt = createdAt
		prepared[index] = current
	}

	return prepared
}

func rollback(file *os.File, offset int64, cause error) error {
	truncateErr := file.Truncate(offset)
	syncErr := file.Sync()

	return errors.Join(cause, truncateErr, syncErr)
}
