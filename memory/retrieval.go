package memory

import (
	"context"
	"errors"
	"slices"
)

const DefaultSearchLimit = 10

var ErrPipelineRequired = errors.New("memory search requires a pipeline")

type Retriever interface {
	Retrieve(ctx context.Context, query Query) ([]Candidate, error)
}

type Pipeline interface {
	Search(ctx context.Context, query Query) ([]SearchResult, error)
}

type Merger interface {
	Merge(ctx context.Context, lists [][]SearchResult) ([]SearchResult, error)
}

type Query struct {
	Text   string   `json:"text"`
	Scopes []string `json:"scopes,omitempty"`
	IDs    []string `json:"ids,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

func (q Query) MatchesFilters(id, scope string) bool {
	return (len(q.Scopes) == 0 || slices.Contains(q.Scopes, scope)) &&
		(len(q.IDs) == 0 || slices.Contains(q.IDs, id))
}

type QueryOption func(*Query)

func NewQuery(text string, options ...QueryOption) Query {
	query := Query{Text: text}
	for _, option := range options {
		option(&query)
	}

	if query.Limit == 0 {
		query.Limit = DefaultSearchLimit
	}

	return query
}

func WithScopes(scopes ...string) QueryOption {
	return func(query *Query) {
		query.Scopes = append([]string(nil), scopes...)
	}
}

func WithIDs(ids ...string) QueryOption {
	return func(query *Query) {
		query.IDs = append([]string(nil), ids...)
	}
}

func WithLimit(limit int) QueryOption {
	return func(query *Query) {
		query.Limit = limit
	}
}

type Candidate struct {
	ID      string  `json:"id"`
	Version uint64  `json:"version,omitempty"`
	Score   float64 `json:"score"`
}

type SearchResult struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score"`
}
