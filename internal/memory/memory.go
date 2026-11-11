package memory

import (
	"context"
	"fmt"
	"log"

	"github.com/vitaliipsl/crappy-memory/index/bm25"
	corememory "github.com/vitaliipsl/crappy-memory/memory"
	"github.com/vitaliipsl/crappy-memory/pipeline"
	jsonstore "github.com/vitaliipsl/crappy-memory/storage/json"
)

func Open(ctx context.Context, config Config) (*corememory.Memory, error) {
	store, err := jsonstore.New(config.Path)
	if err != nil {
		return nil, fmt.Errorf("open memory storage: %w", err)
	}

	lexical, err := openLexicalIndex(ctx, store)
	if err != nil {
		return nil, err
	}

	search, err := pipeline.New(store, pipeline.WithRetrievers(lexical))
	if err != nil {
		return nil, fmt.Errorf("create memory pipeline: %w", err)
	}

	onIndexError := func(_ context.Context, err error) {
		log.Printf("memory indexing: %v", err)
	}

	return corememory.New(store,
		corememory.WithIndexer(lexical, onIndexError),
		corememory.WithPipeline(search),
	), nil
}

func openLexicalIndex(ctx context.Context, store corememory.Store) (*bm25.Index, error) {
	index, err := bm25.New(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("initialize memory text index: %w", err)
	}

	return index, nil
}
