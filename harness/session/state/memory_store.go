package state

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"sync"
)

var _ Store = (*MemoryStore)(nil)

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

func (s *MemoryStore) Get(ctx context.Context, sessionID string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	record, found := s.records[sessionID]
	if !found {
		return Record{}, ErrNotFound
	}

	return copyRecord(record), nil
}

func (s *MemoryStore) Save(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	s.records[record.ID] = copyRecord(record)

	return nil
}

func (s *MemoryStore) Update(ctx context.Context, sessionID string, update func(*Record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, found := s.records[sessionID]
	if !found {
		return ErrNotFound
	}

	record = copyRecord(record)
	if err := update(&record); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	s.records[sessionID] = copyRecord(record)

	return nil
}

func (s *MemoryStore) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, copyRecord(record))
	}

	slices.SortFunc(records, func(a, b Record) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})

	return records, nil
}

func (s *MemoryStore) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	delete(s.records, sessionID)

	return nil
}

func copyRecord(record Record) Record {
	if record.Values == nil {
		return record
	}

	values := make(map[string]json.RawMessage, len(record.Values))
	for key, value := range record.Values {
		values[key] = bytes.Clone(value)
	}

	record.Values = values

	return record
}
