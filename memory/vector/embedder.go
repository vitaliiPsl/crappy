package vector

import (
	"context"
	"encoding/json"
)

type Embedding struct {
	Values   []float32                  `json:"values"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
}

type Embedder interface {
	Space() string
	EmbedQuery(ctx context.Context, text string) (Embedding, error)
	EmbedDocuments(ctx context.Context, texts []string) ([]Embedding, error)
}
