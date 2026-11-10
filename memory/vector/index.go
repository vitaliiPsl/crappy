package vector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vitaliipsl/crappy/memory"
)

var (
	_ memory.Indexer   = (*Index)(nil)
	_ memory.Retriever = (*Index)(nil)
)

type Index struct {
	store    Store
	embedder Embedder
	space    string
}

// New requires stable canonical and vector stores during reconciliation.
func New(ctx context.Context, entries memory.Store, store Store, embedder Embedder) (*Index, error) {
	space := embedder.Space()
	if strings.TrimSpace(space) == "" {
		return nil, errors.New("embedding space is required")
	}

	index := &Index{
		store:    store,
		embedder: embedder,
		space:    space,
	}

	if err := index.reconcile(ctx, entries); err != nil {
		return nil, err
	}

	if err := index.populate(ctx, entries); err != nil {
		return nil, err
	}

	return index, nil
}

func (i *Index) Upsert(ctx context.Context, entry memory.Entry) error {
	return i.upsert(ctx, []memory.Entry{entry})
}

func (i *Index) Remove(ctx context.Context, id string) error {
	return i.store.Remove(ctx, id)
}

func (i *Index) Retrieve(ctx context.Context, query memory.Query) ([]memory.Candidate, error) {
	if query.Limit < 0 {
		return nil, errors.New("retrieval limit cannot be negative")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(query.Text) == "" {
		return []memory.Candidate{}, nil
	}

	embedding, err := i.embedder.EmbedQuery(ctx, query.Text)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	return i.store.Search(ctx, Query{Space: i.space, Values: embedding.Values, Scopes: query.Scopes, IDs: query.IDs, Limit: query.Limit})
}

func (i *Index) reconcile(ctx context.Context, entries memory.Store) error {
	filter := memory.Filter{Limit: memory.DefaultPageSize}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		page, err := i.store.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("list indexed vectors: %w", err)
		}

		for _, record := range page.Records {
			_, err := entries.Get(ctx, record.ID)
			if errors.Is(err, memory.ErrNotFound) {
				if err := i.store.Remove(ctx, record.ID); err != nil {
					return fmt.Errorf("remove orphaned vector %q: %w", record.ID, err)
				}

				continue
			}

			if err != nil {
				return fmt.Errorf("load indexed entry %q: %w", record.ID, err)
			}
		}

		if page.NextID == "" {
			return nil
		}

		filter.AfterID = page.NextID
	}
}

func (i *Index) populate(ctx context.Context, entries memory.Store) error {
	filter := memory.Filter{Limit: memory.DefaultPageSize}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		page, err := entries.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("load vector entries: %w", err)
		}

		if err := i.upsert(ctx, page.Entries); err != nil {
			return err
		}

		if page.NextID == "" {
			return nil
		}

		filter.AfterID = page.NextID
	}
}

func (i *Index) upsert(ctx context.Context, entries []memory.Entry) error {
	pending := make([]memory.Entry, 0, len(entries))

	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Version == 0 {
			return errors.New("indexed entry ID and version are required")
		}

		record, err := i.store.Get(ctx, entry.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("load vector %q: %w", entry.ID, err)
		}

		if err == nil && record.Version == entry.Version && record.Space == i.space {
			continue
		}

		pending = append(pending, entry)
		texts = append(texts, entry.Content)
	}

	if len(pending) == 0 {
		return nil
	}

	embeddings, err := i.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed entries: %w", err)
	}

	if len(embeddings) != len(pending) {
		return fmt.Errorf("embedder returned %d vectors for %d entries", len(embeddings), len(pending))
	}

	records := make([]Record, len(pending))
	for position, entry := range pending {
		embedding := embeddings[position]
		records[position] = Record{
			ID:       entry.ID,
			Scope:    entry.Scope,
			Version:  entry.Version,
			Space:    i.space,
			Values:   embedding.Values,
			Metadata: embedding.Metadata,
		}
	}

	if err := i.store.Upsert(ctx, records); err != nil {
		return fmt.Errorf("store embedded entries: %w", err)
	}

	return nil
}
