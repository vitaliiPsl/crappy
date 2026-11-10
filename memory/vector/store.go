package vector

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/vitaliipsl/crappy/memory"
)

var ErrNotFound = errors.New("vector record not found")

type Record struct {
	ID       string                     `json:"id"`
	Scope    string                     `json:"scope"`
	Version  uint64                     `json:"version"`
	Space    string                     `json:"space"`
	Values   []float32                  `json:"values"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
}

type Page struct {
	Records []Record `json:"records"`
	NextID  string   `json:"next_id,omitempty"`
}

type Query struct {
	Space  string    `json:"space"`
	Values []float32 `json:"values"`
	Scopes []string  `json:"scopes,omitempty"`
	IDs    []string  `json:"ids,omitempty"`
	Limit  int       `json:"limit,omitempty"`
}

type Store interface {
	Get(ctx context.Context, id string) (Record, error)
	List(ctx context.Context, filter memory.Filter) (Page, error)
	Upsert(ctx context.Context, records []Record) error
	Remove(ctx context.Context, id string) error
	Search(ctx context.Context, query Query) ([]memory.Candidate, error)
}
