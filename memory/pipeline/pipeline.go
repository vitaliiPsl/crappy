package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliipsl/crappy/memory"
)

var _ memory.Pipeline = (*Pipeline)(nil)

const DefaultCandidateLimit = 100

type Option func(*Pipeline)

func WithRetrievers(retrievers ...memory.Retriever) Option {
	return func(pipeline *Pipeline) {
		pipeline.retrievers = append(pipeline.retrievers, retrievers...)
	}
}

func WithMerger(merger memory.Merger) Option {
	return func(pipeline *Pipeline) {
		pipeline.merger = merger
	}
}

// WithCandidateLimit sets the per-retriever budget; a larger result limit raises it.
func WithCandidateLimit(limit int) Option {
	return func(pipeline *Pipeline) {
		pipeline.candidateLimit = limit
	}
}

type Pipeline struct {
	candidateLimit int

	store      memory.Store
	retrievers []memory.Retriever
	merger     memory.Merger
}

func New(store memory.Store, options ...Option) (*Pipeline, error) {
	pipeline := &Pipeline{store: store, candidateLimit: DefaultCandidateLimit}
	for _, option := range options {
		option(pipeline)
	}

	if len(pipeline.retrievers) == 0 {
		return nil, errors.New("at least one retriever is required")
	}

	if len(pipeline.retrievers) > 1 && pipeline.merger == nil {
		return nil, errors.New("multiple retrievers require a merger")
	}

	if pipeline.candidateLimit <= 0 {
		return nil, errors.New("candidate limit must be greater than zero")
	}

	return pipeline, nil
}

func (p *Pipeline) Search(ctx context.Context, query memory.Query) ([]memory.SearchResult, error) {
	if query.Limit < 0 {
		return nil, errors.New("search limit cannot be negative")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if query.Limit == 0 {
		query.Limit = memory.DefaultSearchLimit
	}

	candidates, err := p.retrieve(ctx, query)
	if err != nil {
		return nil, err
	}

	resolved, err := p.resolve(ctx, query, candidates)
	if err != nil {
		return nil, err
	}

	results, err := p.merge(ctx, resolved)
	if err != nil {
		return nil, err
	}

	return results[:min(query.Limit, len(results))], nil
}

func (p *Pipeline) retrieve(ctx context.Context, query memory.Query) ([][]memory.Candidate, error) {
	query.Limit = max(query.Limit, p.candidateLimit)

	lists := make([][]memory.Candidate, len(p.retrievers))
	for position, retriever := range p.retrievers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		candidates, err := retriever.Retrieve(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("retrieve memory candidates from %T: %w", retriever, err)
		}

		lists[position] = candidates
	}

	return lists, nil
}

func (p *Pipeline) resolve(ctx context.Context, query memory.Query, lists [][]memory.Candidate) ([][]memory.SearchResult, error) {
	entries, err := p.resolveEntries(ctx, lists)
	if err != nil {
		return nil, err
	}

	resolved := make([][]memory.SearchResult, len(lists))
	for position, candidates := range lists {
		resolved[position] = resolveCandidates(query, candidates, entries)
	}

	return resolved, nil
}

func (p *Pipeline) merge(ctx context.Context, lists [][]memory.SearchResult) ([]memory.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(lists) == 1 {
		return lists[0], nil
	}

	results, err := p.merger.Merge(ctx, lists)
	if err != nil {
		return nil, fmt.Errorf("merge memory results: %w", err)
	}

	return results, nil
}

func (p *Pipeline) resolveEntries(ctx context.Context, lists [][]memory.Candidate) (map[string]*memory.Entry, error) {
	entries := make(map[string]*memory.Entry)
	for _, candidates := range lists {
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			if _, loaded := entries[candidate.ID]; loaded {
				continue
			}

			entry, err := p.store.Get(ctx, candidate.ID)
			if errors.Is(err, memory.ErrNotFound) {
				entries[candidate.ID] = nil

				continue
			}

			if err != nil {
				return nil, fmt.Errorf("load memory candidate %q: %w", candidate.ID, err)
			}

			entries[candidate.ID] = &entry
		}
	}

	return entries, nil
}

func resolveCandidates(query memory.Query, candidates []memory.Candidate, entries map[string]*memory.Entry) []memory.SearchResult {
	results := make([]memory.SearchResult, 0, len(candidates))

	seen := make(map[string]bool)
	for _, candidate := range candidates {
		if seen[candidate.ID] {
			continue
		}

		entry := entries[candidate.ID]
		if entry == nil {
			continue
		}

		if candidate.Version != 0 && candidate.Version != entry.Version {
			continue
		}

		if !query.MatchesFilters(entry.ID, entry.Scope) {
			continue
		}

		seen[candidate.ID] = true
		results = append(results, memory.SearchResult{Entry: *entry, Score: candidate.Score})
	}

	return results
}
