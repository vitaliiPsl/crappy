package entry

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

var _ Store = (*MemoryStore)(nil)

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string][]Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string][]Entry),
	}
}

func (s *MemoryStore) Append(
	ctx context.Context,
	entries ...Entry,
) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prepared := make([]Entry, len(entries))
	for i, entry := range entries {
		entry.Payload = clonePayload(entry.Payload)
		prepared[i] = entry
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	nextIDs := make(map[string]uint64)
	for i := range prepared {
		sessionID := prepared[i].SessionID

		nextID, ok := nextIDs[sessionID]
		if !ok {
			nextID = uint64(len(s.entries[sessionID])) + 1
		}

		prepared[i].ID = nextID
		prepared[i].CreatedAt = time.Now().UTC()
		nextIDs[sessionID] = nextID + 1
	}

	for _, entry := range prepared {
		s.entries[entry.SessionID] = append(s.entries[entry.SessionID], entry)
	}

	return cloneEntries(prepared), nil
}

func (s *MemoryStore) List(ctx context.Context, sessionID string, afterID uint64) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.entries[sessionID]

	start := len(history)
	for i := range history {
		if history[i].ID > afterID {
			start = i

			break
		}
	}

	return cloneEntries(history[start:]), nil
}

func cloneEntries(entries []Entry) []Entry {
	cloned := make([]Entry, len(entries))
	for i, entry := range entries {
		entry.Payload = clonePayload(entry.Payload)
		cloned[i] = entry
	}

	return cloned
}

func clonePayload(payload json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), payload...)
}
