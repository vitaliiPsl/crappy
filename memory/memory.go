package memory

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Memory struct {
	store Store
}

func New(store Store) *Memory {
	return &Memory{store: store}
}

func (m *Memory) Create(ctx context.Context, scope, content string, options ...EntryOption) (Entry, error) {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(content) == "" {
		return Entry{}, errors.New("memory scope and content are required")
	}

	now := time.Now().UTC()

	entry, err := applyEntryOptions(Entry{
		ID:        uuid.NewString(),
		Scope:     scope,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}, options)
	if err != nil {
		return Entry{}, err
	}

	if err := m.store.Create(ctx, entry); err != nil {
		return Entry{}, err
	}

	return entry, nil
}

func (m *Memory) Get(ctx context.Context, id string) (Entry, error) {
	return m.store.Get(ctx, id)
}

func (m *Memory) Update(ctx context.Context, id string, expectedVersion uint64, content string, options ...EntryOption) (Entry, error) {
	if strings.TrimSpace(content) == "" {
		return Entry{}, errors.New("memory content is required")
	}

	entry, err := m.store.Get(ctx, id)
	if err != nil {
		return Entry{}, err
	}

	entry, err = applyEntryOptions(entry, options)
	if err != nil {
		return Entry{}, err
	}

	entry.Content = content
	entry.UpdatedAt = time.Now().UTC()
	entry.Version = expectedVersion + 1

	if err := m.store.Update(ctx, entry, expectedVersion); err != nil {
		return Entry{}, err
	}

	return entry, nil
}

func (m *Memory) Delete(ctx context.Context, id string, expectedVersion uint64) error {
	err := m.store.Delete(ctx, id, expectedVersion)
	if errors.Is(err, ErrNotFound) {
		return nil
	}

	return err
}

func (m *Memory) List(ctx context.Context, filter Filter) (Page, error) {
	return m.store.List(ctx, filter)
}
