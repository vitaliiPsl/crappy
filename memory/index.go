package memory

import "context"

type Indexer interface {
	Upsert(ctx context.Context, entry Entry) error
	Remove(ctx context.Context, id string) error
}
