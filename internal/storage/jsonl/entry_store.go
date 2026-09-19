package jsonl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

	if state.entries != nil {
		return entriesAfter(state.entries, afterID), nil
	}

	path := filepath.Join(s.storage.root, sessionID, transcriptFilename)

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		state.entries = make([]entry.Entry, 0)

		return []entry.Entry{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}

	defer func() {
		_ = file.Close()
	}()

	entries, err := loadEntries(ctx, file)
	if err != nil {
		return nil, err
	}

	state.entries = entries

	return entriesAfter(state.entries, afterID), nil
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

	if state.entries == nil {
		entries, err := loadEntries(ctx, file)
		if err != nil {
			return nil, err
		}

		state.entries = entries
	}

	prepared := prepareEntries(sessionID, entries, lastEntryID(state.entries))

	encoded, err := encodeEntries(prepared)
	if err != nil {
		return nil, fmt.Errorf("encode transcript entries: %w", err)
	}

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
		state.entries = nil

		return nil, rollback(file, offset, fmt.Errorf("append transcript: %w", err))
	}

	if err := ctx.Err(); err != nil {
		state.entries = nil

		return nil, rollback(file, offset, err)
	}

	if err := file.Sync(); err != nil {
		state.entries = nil

		return nil, rollback(file, offset, fmt.Errorf("sync transcript: %w", err))
	}

	state.entries = append(state.entries, cloneEntries(prepared)...)

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

func entriesAfter(entries []entry.Entry, afterID uint64) []entry.Entry {
	start := sort.Search(len(entries), func(index int) bool {
		return entries[index].ID > afterID
	})

	return cloneEntries(entries[start:])
}

func lastEntryID(entries []entry.Entry) uint64 {
	if len(entries) == 0 {
		return 0
	}

	return entries[len(entries)-1].ID
}

func cloneEntries(entries []entry.Entry) []entry.Entry {
	cloned := make([]entry.Entry, len(entries))
	for index, current := range entries {
		current.Payload = append([]byte(nil), current.Payload...)
		cloned[index] = current
	}

	return cloned
}

func rollback(file *os.File, offset int64, cause error) error {
	truncateErr := file.Truncate(offset)
	syncErr := file.Sync()

	return errors.Join(cause, truncateErr, syncErr)
}
