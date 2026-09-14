package entry

import "context"

type Store interface {
	Append(ctx context.Context, entries ...Entry) ([]Entry, error)
	List(ctx context.Context, sessionID string, afterID uint64) ([]Entry, error)
}
