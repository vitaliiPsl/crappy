package session

import "context"

type Store interface {
	List(ctx context.Context) ([]State, error)
}
