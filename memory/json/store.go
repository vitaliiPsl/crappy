package json

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/vitaliipsl/crappy/memory"
)

var _ memory.Store = (*Store)(nil)

type Store struct {
	path string

	mu      sync.RWMutex
	entries map[string]memory.Entry
	scopes  map[string]map[string]struct{}
}

func New(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("memory file path is required")
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve memory file path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}

	store := &Store{
		path:    path,
		entries: make(map[string]memory.Entry),
		scopes:  make(map[string]map[string]struct{}),
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read memory file: %w", err)
	}

	if err := json.Unmarshal(data, &store.entries); err != nil {
		return nil, fmt.Errorf("decode memory file: %w", err)
	}

	if store.entries == nil {
		return nil, errors.New("memory file must be a JSON object")
	}

	for id, entry := range store.entries {
		if id == "" || entry.ID != id {
			return nil, fmt.Errorf("memory entry ID %q does not match key %q", entry.ID, id)
		}

		if entry.Version == 0 {
			return nil, fmt.Errorf("memory entry %q has no version", id)
		}

		store.addToScope(entry)
	}

	return store, nil
}

func (s *Store) Create(ctx context.Context, entry memory.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if entry.ID == "" || entry.Version == 0 {
		return errors.New("memory entry ID and version are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, found := s.entries[entry.ID]; found {
		return memory.ErrAlreadyExists
	}

	entry = copyEntry(entry)

	entries := maps.Clone(s.entries)
	entries[entry.ID] = entry

	if err := s.write(ctx, entries); err != nil {
		return err
	}

	s.entries = entries
	s.addToScope(entry)

	return s.sync()
}

func (s *Store) Get(ctx context.Context, id string) (memory.Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return memory.Entry{}, err
	}

	entry, found := s.entries[id]
	if !found {
		return memory.Entry{}, memory.ErrNotFound
	}

	return copyEntry(entry), nil
}

func (s *Store) Update(ctx context.Context, entry memory.Entry, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous, found := s.entries[entry.ID]
	if !found {
		return memory.ErrNotFound
	}

	if previous.Version != expectedVersion {
		return memory.ErrConflict
	}

	if entry.Version <= expectedVersion {
		return errors.New("replacement memory entry version must increase")
	}

	entry = copyEntry(entry)

	entries := maps.Clone(s.entries)
	entries[entry.ID] = entry

	if err := s.write(ctx, entries); err != nil {
		return err
	}

	s.entries = entries
	s.removeFromScope(previous)
	s.addToScope(entry)

	return s.sync()
}

func (s *Store) Delete(ctx context.Context, id string, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, found := s.entries[id]
	if !found {
		return memory.ErrNotFound
	}

	if entry.Version != expectedVersion {
		return memory.ErrConflict
	}

	entries := maps.Clone(s.entries)
	delete(entries, id)

	if err := s.write(ctx, entries); err != nil {
		return err
	}

	s.entries = entries
	s.removeFromScope(entry)

	return s.sync()
}

func (s *Store) List(ctx context.Context, filter memory.Filter) (memory.Page, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return memory.Page{}, err
	}

	if filter.Limit < 0 {
		return memory.Page{}, errors.New("memory list limit cannot be negative")
	}

	limit := filter.Limit
	if limit == 0 {
		limit = memory.DefaultPageSize
	}

	ids := s.matchingIDs(filter)

	page := memory.Page{Entries: make([]memory.Entry, 0, min(len(ids), limit))}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return memory.Page{}, err
		}

		entry, found := s.entries[id]
		if !found || id <= filter.AfterID || (len(filter.Scopes) != 0 && !slices.Contains(filter.Scopes, entry.Scope)) {
			continue
		}

		if len(page.Entries) == limit {
			page.NextID = page.Entries[len(page.Entries)-1].ID

			break
		}

		page.Entries = append(page.Entries, copyEntry(entry))
	}

	return page, nil
}

func (s *Store) matchingIDs(filter memory.Filter) []string {
	if len(filter.IDs) != 0 {
		return slices.Compact(slices.Sorted(slices.Values(filter.IDs)))
	}

	if len(filter.Scopes) == 0 {
		return slices.Sorted(maps.Keys(s.entries))
	}

	ids := make(map[string]struct{})
	for _, scope := range filter.Scopes {
		for id := range s.scopes[scope] {
			ids[id] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(ids))
}

func (s *Store) addToScope(entry memory.Entry) {
	if s.scopes[entry.Scope] == nil {
		s.scopes[entry.Scope] = make(map[string]struct{})
	}

	s.scopes[entry.Scope][entry.ID] = struct{}{}
}

func (s *Store) removeFromScope(entry memory.Entry) {
	ids := s.scopes[entry.Scope]
	delete(ids, entry.ID)

	if len(ids) == 0 {
		delete(s.scopes, entry.Scope)
	}
}

func (s *Store) write(ctx context.Context, entries map[string]memory.Entry) (resultErr error) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory file: %w", err)
	}

	file, err := os.CreateTemp(filepath.Dir(s.path), ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("create memory file: %w", err)
	}

	defer func() {
		if resultErr != nil {
			_ = os.Remove(file.Name())
		}
	}()

	if _, err := file.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write memory file: %w", err), file.Close())
	}

	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync memory file: %w", err), file.Close())
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close memory file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Rename(file.Name(), s.path); err != nil {
		return fmt.Errorf("replace memory file: %w", err)
	}

	return nil
}

func (s *Store) sync() error {
	directory, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return fmt.Errorf("open memory directory: %w", err)
	}

	return errors.Join(directory.Sync(), directory.Close())
}

func copyEntry(entry memory.Entry) memory.Entry {
	if entry.Metadata == nil {
		return entry
	}

	metadata := make(map[string]json.RawMessage, len(entry.Metadata))
	for key, value := range entry.Metadata {
		metadata[key] = bytes.Clone(value)
	}

	entry.Metadata = metadata

	return entry
}
