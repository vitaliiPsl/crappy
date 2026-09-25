package session

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("session not found")

type State struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store interface {
	Get(ctx context.Context, sessionID string) (State, error)
	Save(ctx context.Context, state State) error
	List(ctx context.Context) ([]State, error)
	Delete(ctx context.Context, sessionID string) error
}
