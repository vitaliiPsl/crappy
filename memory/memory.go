package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Memory struct {
	store    Store
	indexers []indexer
	pipeline Pipeline
}

type indexer struct {
	index   Indexer
	onError func(context.Context, error)
}

type Option func(*Memory)

func WithIndexer(index Indexer, onError func(context.Context, error)) Option {
	return func(m *Memory) {
		m.indexers = append(m.indexers, indexer{index: index, onError: onError})
	}
}

func WithPipeline(pipeline Pipeline) Option {
	return func(m *Memory) {
		m.pipeline = pipeline
	}
}

func New(store Store, options ...Option) *Memory {
	m := &Memory{store: store}
	for _, option := range options {
		option(m)
	}

	return m
}

func (m *Memory) Create(ctx context.Context, scope, content string, options ...EntryOption) (Entry, error) {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(content) == "" {
		return Entry{}, errors.New("memory scope and content are required")
	}

	now := time.Now().UTC()

	entry := Entry{
		ID:        uuid.NewString(),
		Scope:     scope,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}

	if err := applyEntryOptions(&entry, options); err != nil {
		return Entry{}, err
	}

	if err := m.store.Create(ctx, entry); err != nil {
		return Entry{}, err
	}

	m.upsert(ctx, entry)

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

	if err := applyEntryOptions(&entry, options); err != nil {
		return Entry{}, err
	}

	entry.Content = content
	entry.UpdatedAt = time.Now().UTC()
	entry.Version = expectedVersion + 1

	if err := m.store.Update(ctx, entry, expectedVersion); err != nil {
		return Entry{}, err
	}

	m.upsert(ctx, entry)

	return entry, nil
}

func (m *Memory) Delete(ctx context.Context, id string, expectedVersion uint64) error {
	err := m.store.Delete(ctx, id, expectedVersion)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	for _, indexer := range m.indexers {
		if err := indexer.index.Remove(ctx, id); err != nil {
			indexer.onError(ctx, fmt.Errorf("remove memory %q from %T: %w", id, indexer.index, err))
		}
	}

	return nil
}

func (m *Memory) List(ctx context.Context, filter Filter) (Page, error) {
	return m.store.List(ctx, filter)
}

func (m *Memory) Search(ctx context.Context, text string, options ...QueryOption) ([]SearchResult, error) {
	if m.pipeline == nil {
		return nil, ErrPipelineRequired
	}

	return m.pipeline.Search(ctx, NewQuery(text, options...))
}

func (m *Memory) upsert(ctx context.Context, entry Entry) {
	for _, indexer := range m.indexers {
		if err := indexer.index.Upsert(ctx, entry); err != nil {
			indexer.onError(ctx, fmt.Errorf("index memory %q at version %d in %T: %w", entry.ID, entry.Version, indexer.index, err))
		}
	}
}
