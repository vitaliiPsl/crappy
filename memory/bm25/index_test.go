package bm25_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
)

func TestIndexRanksTermsAndReturnsVersionedCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	index := bm25.New()
	for _, entry := range []memory.Entry{
		{ID: "a", Scope: "personal", Content: "Alpha", Version: 2},
		{ID: "b", Scope: "personal", Content: "Beta", Version: 1},
	} {
		if err := index.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("ALPHA alpha"))
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 1 || candidates[0].ID != "a" || candidates[0].Version != 2 || math.Abs(candidates[0].Score-math.Log(2)) > 1e-12 {
		t.Fatalf("candidates = %+v, want entry a with score ln(2)", candidates)
	}

	for _, entry := range []memory.Entry{
		{ID: "short", Content: "tool tool", Version: 1},
		{ID: "long", Content: "tool " + strings.Repeat("unrelated ", 20), Version: 1},
		{ID: "unicode", Content: "MÉMOIRE, 世界!", Version: 1},
	} {
		if err := index.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("tool"))
	if err != nil || len(candidates) != 2 || candidates[0].ID != "short" || candidates[0].Score <= candidates[1].Score {
		t.Fatalf("term frequency and length ranking = %+v, %v", candidates, err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("mémoire 世界"))
	if err != nil || len(candidates) != 1 || candidates[0].ID != "unicode" {
		t.Fatalf("unicode query = %+v, %v", candidates, err)
	}
}

func TestIndexFiltersScopesAndIDsBeforeLimiting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	index := bm25.New()
	for _, entry := range []memory.Entry{
		{ID: "a", Scope: "workdir:/one/child", Content: "same", Version: 1},
		{ID: "b", Scope: "workdir:/one", Content: "same", Version: 1},
		{ID: "c", Scope: "workdir:/one", Content: "same", Version: 1},
	} {
		if err := index.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("same", memory.WithScopes("workdir:/one"), memory.WithLimit(1)))
	if err != nil || len(candidates) != 1 || candidates[0].ID != "b" {
		t.Fatalf("scoped result limit or tie order = %+v, %v", candidates, err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("same", memory.WithScopes("workdir:/one"), memory.WithIDs("c")))
	if err != nil || len(candidates) != 1 || candidates[0].ID != "c" {
		t.Fatalf("ID filter = %+v, %v", candidates, err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("", memory.WithLimit(1)))
	if err != nil || len(candidates) != 0 {
		t.Fatalf("empty query = %+v, %v", candidates, err)
	}

	if _, err := index.Retrieve(ctx, memory.NewQuery("same", memory.WithLimit(-1))); err == nil {
		t.Fatal("negative retrieval limit was accepted")
	}
}

func TestIndexIgnoresRepeatedAndOutOfOrderChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	index := bm25.New()
	for _, entry := range []memory.Entry{
		{ID: "note", Content: "current", Version: 2},
		{ID: "note", Content: "stale", Version: 1},
		{ID: "note", Content: "duplicate", Version: 2},
	} {
		if err := index.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	if err := index.Remove(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("current stale duplicate"))
	if err != nil || len(candidates) != 1 || candidates[0].Version != 2 {
		t.Fatalf("older change replaced the current entry: %+v, %v", candidates, err)
	}

	if err := index.Remove(ctx, "note", 2); err != nil {
		t.Fatal(err)
	}

	if err := index.Upsert(ctx, memory.Entry{ID: "note", Content: "current", Version: 2}); err != nil {
		t.Fatal(err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("current"))
	if err != nil || len(candidates) != 0 {
		t.Fatalf("delayed upsert resurrected a removed version: %+v, %v", candidates, err)
	}

	if err := index.Remove(ctx, "unseen", 4); err != nil {
		t.Fatal(err)
	}

	if err := index.Upsert(ctx, memory.Entry{ID: "unseen", Content: "late", Version: 3}); err != nil {
		t.Fatal(err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("late"))
	if err != nil || len(candidates) != 0 {
		t.Fatalf("upsert older than an unseen removal was indexed: %+v, %v", candidates, err)
	}

	if err := index.Upsert(ctx, memory.Entry{ID: "note", Content: "newer", Version: 3}); err != nil {
		t.Fatal(err)
	}

	candidates, err = index.Retrieve(ctx, memory.NewQuery("newer"))
	if err != nil || len(candidates) != 1 || candidates[0].Version != 3 {
		t.Fatalf("newer version was lost: %+v, %v", candidates, err)
	}
}

func TestIndexCanceledOperationsPreserveState(t *testing.T) {
	t.Parallel()

	index := bm25.New()

	entry := memory.Entry{ID: "note", Content: "original", Version: 1}
	if err := index.Upsert(t.Context(), entry); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := index.Upsert(ctx, memory.Entry{ID: entry.ID, Content: "changed", Version: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upsert: %v", err)
	}

	if err := index.Remove(ctx, entry.ID, entry.Version); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled remove: %v", err)
	}

	if _, err := index.Retrieve(ctx, memory.NewQuery("original")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retrieval: %v", err)
	}

	candidates, err := index.Retrieve(t.Context(), memory.NewQuery("original changed"))
	if err != nil || len(candidates) != 1 || candidates[0].Version != 1 {
		t.Fatalf("canceled operations changed the index: %+v, %v", candidates, err)
	}
}

func TestIndexConcurrentUpdatesKeepNewestVersion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	index := bm25.New()

	var wg sync.WaitGroup
	for version := uint64(1); version <= 20; version++ {
		wg.Go(func() {
			if err := index.Upsert(ctx, memory.Entry{ID: "note", Content: "shared", Version: version}); err != nil {
				t.Error(err)
			}

			if _, err := index.Retrieve(ctx, memory.NewQuery("shared")); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	candidates, err := index.Retrieve(ctx, memory.NewQuery("shared"))
	if err != nil || len(candidates) != 1 || candidates[0].Version != 20 {
		t.Fatalf("newest version was lost: %+v, %v", candidates, err)
	}
}
