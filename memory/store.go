package memory

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("memory entry not found")
	ErrAlreadyExists = errors.New("memory entry already exists")
	ErrConflict      = errors.New("memory entry version conflict")
)

type Store interface {
	Get(ctx context.Context, id string) (Entry, error)
	List(ctx context.Context, filter Filter) (Page, error)
	Create(ctx context.Context, entry Entry) error
	Update(ctx context.Context, entry Entry, expectedVersion uint64) error
	Delete(ctx context.Context, id string, expectedVersion uint64) error
}
