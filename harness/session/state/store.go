package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("session not found")

type Record struct {
	ID        string                     `json:"id"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Values    map[string]json.RawMessage `json:"values,omitempty"`
}

type Store interface {
	Get(ctx context.Context, sessionID string) (Record, error)
	Save(ctx context.Context, record Record) error
	Update(ctx context.Context, sessionID string, update func(*Record) error) error
	List(ctx context.Context) ([]Record, error)
	Delete(ctx context.Context, sessionID string) error
}
