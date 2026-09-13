package entry

import "context"

type Store interface {
	// Append stores entries and returns their durable forms.
	// The store assigns ID and CreatedAt.
	Append(ctx context.Context, entries ...Entry) ([]Entry, error)

	// List returns entries with an ID greater than afterID.
	// Passing zero returns the complete Session history.
	List(ctx context.Context, sessionID string, afterID uint64) ([]Entry, error)
}
