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
	store          Store
	indexers       []indexer
	retriever      Retriever
	candidateLimit int
}

type indexer struct {
	index   Indexer
	onError func(context.Context, error)
}

type Option func(*Memory)

func New(store Store, options ...Option) *Memory {
	m := &Memory{store: store, candidateLimit: DefaultCandidateLimit}
	for _, option := range options {
		option(m)
	}

	return m
}

func WithIndexer(index Indexer, onError func(context.Context, error)) Option {
	return func(m *Memory) {
		m.indexers = append(m.indexers, indexer{index: index, onError: onError})
	}
}

func WithRetriever(retriever Retriever) Option {
	return func(m *Memory) {
		m.retriever = retriever
	}
}

// WithCandidateLimit sets the candidate budget; a larger result limit raises it.
func WithCandidateLimit(limit int) Option {
	return func(m *Memory) {
		m.candidateLimit = limit
	}
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
		if err := indexer.index.Remove(ctx, id, expectedVersion); err != nil {
			indexer.onError(ctx, fmt.Errorf("remove memory %q through version %d from %T: %w", id, expectedVersion, indexer.index, err))
		}
	}

	return nil
}

func (m *Memory) List(ctx context.Context, filter Filter) (Page, error) {
	return m.store.List(ctx, filter)
}

func (m *Memory) Search(ctx context.Context, text string, options ...QueryOption) ([]SearchResult, error) {
	if m.retriever == nil {
		return nil, ErrRetrieverRequired
	}

	query := NewQuery(text, options...)
	if query.Limit < 0 {
		return nil, errors.New("search limit cannot be negative")
	}

	if m.candidateLimit <= 0 {
		return nil, errors.New("candidate limit must be greater than zero")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	limit := query.Limit
	query.Limit = max(limit, m.candidateLimit)

	candidates, err := m.retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("retrieve memory candidates: %w", err)
	}

	results := make([]SearchResult, 0, min(limit, len(candidates)))

	seen := make(map[string]bool)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if seen[candidate.ID] {
			continue
		}

		entry, err := m.store.Get(ctx, candidate.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("load memory candidate %q: %w", candidate.ID, err)
		}

		if candidate.Version != 0 && candidate.Version != entry.Version {
			continue
		}

		if !query.MatchesFilters(entry.ID, entry.Scope) {
			continue
		}

		seen[entry.ID] = true

		results = append(results, SearchResult{Entry: entry, Score: candidate.Score})
		if len(results) == limit {
			break
		}
	}

	return results, nil
}

func (m *Memory) upsert(ctx context.Context, entry Entry) {
	for _, indexer := range m.indexers {
		if err := indexer.index.Upsert(ctx, entry); err != nil {
			indexer.onError(ctx, fmt.Errorf("index memory %q at version %d in %T: %w", entry.ID, entry.Version, indexer.index, err))
		}
	}
}
