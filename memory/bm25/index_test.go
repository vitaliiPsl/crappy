package bm25_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
)

func TestIndexRanksTermsAndReturnsVersionedCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	index := newIndex(t)
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

func TestIndexConfiguresSaturation(t *testing.T) {
	t.Parallel()

	idf := math.Log(1.2)
	for _, test := range []struct {
		name          string
		saturation    float64
		firstID       string
		repeatedScore float64
	}{
		{name: "disabled", saturation: 0, firstID: "a-once", repeatedScore: idf},
		{name: "custom", saturation: 2, firstID: "b-repeated", repeatedScore: idf * 30 / 11},
		{name: "large finite", saturation: math.MaxFloat64, firstID: "b-repeated", repeatedScore: idf * 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			index := newIndex(t, bm25.WithSaturation(test.saturation))
			for _, entry := range []memory.Entry{
				{ID: "a-once", Content: "term " + strings.Repeat("filler ", 19), Version: 1},
				{ID: "b-repeated", Content: strings.Repeat("term ", 20), Version: 1},
			} {
				if err := index.Upsert(t.Context(), entry); err != nil {
					t.Fatal(err)
				}
			}

			candidates, err := index.Retrieve(t.Context(), memory.NewQuery("term"))
			if err != nil || len(candidates) != 2 || candidates[0].ID != test.firstID {
				t.Fatalf("configured saturation ranking = %+v, %v", candidates, err)
			}

			for _, candidate := range candidates {
				want := idf
				if candidate.ID == "b-repeated" {
					want = test.repeatedScore
				}

				if math.IsNaN(candidate.Score) || math.Abs(candidate.Score-want) > 1e-12 {
					t.Fatalf("candidate %s score = %g, want %g", candidate.ID, candidate.Score, want)
				}
			}
		})
	}
}

func TestIndexConfiguresLengthNormalization(t *testing.T) {
	t.Parallel()

	idf := math.Log(1.2)
	for _, test := range []struct {
		name          string
		normalization float64
		firstID       string
		shortScore    float64
		longScore     float64
	}{
		{name: "disabled", normalization: 0, firstID: "a-long", shortScore: idf, longScore: idf},
		{name: "partial", normalization: 0.5, firstID: "b-short", shortScore: idf * 12 / 11, longScore: idf * 12 / 13},
		{name: "full", normalization: 1, firstID: "b-short", shortScore: idf * 6 / 5, longScore: idf * 6 / 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			index := newIndex(t, bm25.WithSaturation(1), bm25.WithLengthNormalization(test.normalization))
			for _, entry := range []memory.Entry{
				{ID: "a-long", Content: "term filler filler filler", Version: 1},
				{ID: "b-short", Content: "term filler", Version: 1},
			} {
				if err := index.Upsert(t.Context(), entry); err != nil {
					t.Fatal(err)
				}
			}

			candidates, err := index.Retrieve(t.Context(), memory.NewQuery("term"))
			if err != nil || len(candidates) != 2 || candidates[0].ID != test.firstID {
				t.Fatalf("configured length normalization ranking = %+v, %v", candidates, err)
			}

			for _, candidate := range candidates {
				want := test.longScore
				if candidate.ID == "b-short" {
					want = test.shortScore
				}

				if math.IsNaN(candidate.Score) || math.Abs(candidate.Score-want) > 1e-12 {
					t.Fatalf("candidate %s score = %g, want %g", candidate.ID, candidate.Score, want)
				}
			}
		})
	}
}

func TestNewRejectsInvalidScoringOptionsBeforeLoading(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		option bm25.Option
	}{
		{name: "negative saturation", option: bm25.WithSaturation(-1)},
		{name: "NaN saturation", option: bm25.WithSaturation(math.NaN())},
		{name: "positive infinite saturation", option: bm25.WithSaturation(math.Inf(1))},
		{name: "negative infinite saturation", option: bm25.WithSaturation(math.Inf(-1))},
		{name: "negative normalization", option: bm25.WithLengthNormalization(-0.1)},
		{name: "excessive normalization", option: bm25.WithLengthNormalization(1.1)},
		{name: "NaN normalization", option: bm25.WithLengthNormalization(math.NaN())},
		{name: "positive infinite normalization", option: bm25.WithLengthNormalization(math.Inf(1))},
		{name: "negative infinite normalization", option: bm25.WithLengthNormalization(math.Inf(-1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newListStore(func(context.Context, memory.Filter) (memory.Page, error) {
				t.Fatal("invalid scoring options caused a store read")

				return memory.Page{}, nil
			})
			if index, err := bm25.New(t.Context(), store, test.option); index != nil || err == nil {
				t.Fatalf("invalid scoring options: index=%v, error=%v", index, err)
			}
		})
	}
}

func TestIndexFiltersScopesAndIDsBeforeLimiting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	index := newIndex(t)
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

	index := newIndex(t)
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

	index := newIndex(t)

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
	index := newIndex(t)

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

func TestNewLoadsAllPages(t *testing.T) {
	t.Parallel()

	entries := []memory.Entry{
		{ID: "a", Scope: "personal", Content: "stored", Version: 3},
		{ID: "b", Scope: "project:demo", Content: "stored", Version: 2},
		{ID: "c", Scope: "project:demo/child", Content: "stored", Version: 7},
	}
	pages := []memory.Page{
		{Entries: entries[:2], NextID: "b"},
		{Entries: entries[2:]},
	}
	cursors := []string{"", "b"}
	calls := 0
	store := newListStore(func(_ context.Context, filter memory.Filter) (memory.Page, error) {
		if calls >= len(pages) {
			t.Fatal("initialization read beyond the last page")
		}

		if filter.AfterID != cursors[calls] || filter.Limit != memory.DefaultPageSize || len(filter.Scopes) != 0 || len(filter.IDs) != 0 {
			t.Fatalf("initialization filter = %+v", filter)
		}

		page := pages[calls]
		calls++

		return page, nil
	})

	index, err := bm25.New(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := index.Retrieve(t.Context(), memory.NewQuery("stored"))
	if err != nil || len(candidates) != len(entries) || calls != len(pages) {
		t.Fatalf("initialized candidates = %+v, %v; pages=%d", candidates, err, calls)
	}

	for position, candidate := range candidates {
		if candidate.ID != entries[position].ID || candidate.Version != entries[position].Version {
			t.Fatalf("initialized candidate = %+v, want %+v", candidate, entries[position])
		}
	}

	candidates, err = index.Retrieve(t.Context(), memory.NewQuery("stored", memory.WithScopes("project:demo")))
	if err != nil || len(candidates) != 1 || candidates[0].ID != "b" {
		t.Fatalf("initialized scopes = %+v, %v", candidates, err)
	}
}

func TestNewRestoresCurrentEntriesAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "entries.json")

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	index, err := bm25.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.New(store,
		memory.WithIndexer(index, func(_ context.Context, err error) { t.Error(err) }),
		memory.WithRetriever(index),
	)

	original, err := mem.Create(ctx, "personal", "obsolete")
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := mem.Create(ctx, "personal", "forgotten")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := mem.Update(ctx, original.ID, original.Version, "current")
	if err != nil {
		t.Fatal(err)
	}

	if err := mem.Delete(ctx, deleted.ID, deleted.Version); err != nil {
		t.Fatal(err)
	}

	reopened, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := bm25.New(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}

	if err := restored.Upsert(ctx, original); err != nil {
		t.Fatal(err)
	}

	mem = memory.New(reopened, memory.WithRetriever(restored))

	results, err := mem.Search(ctx, "current")
	if err != nil || len(results) != 1 || results[0].Entry.ID != updated.ID || results[0].Entry.Version != updated.Version {
		t.Fatalf("search after restart = %+v, %v", results, err)
	}

	results, err = mem.Search(ctx, "obsolete forgotten")
	if err != nil || len(results) != 0 {
		t.Fatalf("outdated or deleted content survived rebuilding: %+v, %v", results, err)
	}
}

func TestNewDoesNotReturnPartialIndexOnFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("store unavailable")

	store := newListStore(func(_ context.Context, filter memory.Filter) (memory.Page, error) {
		if filter.AfterID == "" {
			return memory.Page{
				Entries: []memory.Entry{{ID: "first", Content: "stored", Version: 1}},
				NextID:  "first",
			}, nil
		}

		return memory.Page{}, failure
	})
	if index, err := bm25.New(t.Context(), store); index != nil || !errors.Is(err, failure) {
		t.Fatalf("failed initialization = %v, %v", index, err)
	}

	store = newListStore(func(context.Context, memory.Filter) (memory.Page, error) {
		return memory.Page{Entries: []memory.Entry{
			{ID: "first", Content: "stored", Version: 1},
			{ID: "invalid", Content: "stored"},
		}}, nil
	})
	if index, err := bm25.New(t.Context(), store); index != nil || err == nil {
		t.Fatalf("invalid entry initialization = %v, %v", index, err)
	}
}

func TestNewHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := newListStore(func(context.Context, memory.Filter) (memory.Page, error) {
		t.Fatal("canceled initialization read the store")

		return memory.Page{}, nil
	})
	if index, err := bm25.New(ctx, store); index != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled initialization = %v, %v", index, err)
	}

	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()

	store = newListStore(func(_ context.Context, filter memory.Filter) (memory.Page, error) {
		if filter.AfterID == "" {
			return memory.Page{
				Entries: []memory.Entry{{ID: "first", Content: "stored", Version: 1}},
				NextID:  "first",
			}, nil
		}

		cancel()

		return memory.Page{Entries: []memory.Entry{{ID: "second", Content: "stored", Version: 1}}}, nil
	})
	if index, err := bm25.New(ctx, store); index != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("initialization canceled between pages = %v, %v", index, err)
	}
}

type listStore struct {
	memory.Store
	list func(context.Context, memory.Filter) (memory.Page, error)
}

func newListStore(list func(context.Context, memory.Filter) (memory.Page, error)) *listStore {
	return &listStore{list: list}
}

func (s *listStore) List(ctx context.Context, filter memory.Filter) (memory.Page, error) {
	return s.list(ctx, filter)
}

func newIndex(t *testing.T, options ...bm25.Option) *bm25.Index {
	t.Helper()

	store, err := jsonstore.New(filepath.Join(t.TempDir(), "entries.json"))
	if err != nil {
		t.Fatal(err)
	}

	index, err := bm25.New(t.Context(), store, options...)
	if err != nil {
		t.Fatal(err)
	}

	return index
}
