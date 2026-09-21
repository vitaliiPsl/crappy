package session

import "context"

type Store interface {
	List(ctx context.Context) ([]State, error)
	Delete(ctx context.Context, sessionID string) error
}
