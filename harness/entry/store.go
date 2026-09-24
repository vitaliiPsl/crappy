package entry

import "context"

type Store interface {
	Append(ctx context.Context, sessionID string, entries ...Entry) ([]Entry, error)
	List(ctx context.Context, sessionID string, afterID uint64) ([]Entry, error)
	Latest(ctx context.Context, sessionID, entryType string) (Entry, bool, error)
}
