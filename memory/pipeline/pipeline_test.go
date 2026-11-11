package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
	"github.com/vitaliipsl/crappy/memory/pipeline"
	"github.com/vitaliipsl/crappy/memory/vector"
	"github.com/vitaliipsl/crappy/memory/vector/inmemory"
)

func TestPipelineSearchResolvesAndFiltersCandidates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	var entries []memory.Entry
	for _, scope := range []string{"selected", "selected", "selected/child", "selected", "selected"} {
		entry, err := owner.Create(ctx, scope, "canonical content", memory.WithSource("original"), memory.WithMetadata("kind", "note"))
		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	first, second, wrongScope, excluded, stale := entries[0], entries[1], entries[2], entries[3], entries[4]
	if _, err := owner.Update(ctx, stale.ID, stale.Version, "newer content"); err != nil {
		t.Fatal(err)
	}

	ids := []string{first.ID, second.ID, wrongScope.ID, stale.ID}
	candidates := []memory.Candidate{
		{ID: "deleted", Version: 1, Score: 100},
		{ID: wrongScope.ID, Version: wrongScope.Version, Score: 90},
		{ID: stale.ID, Version: stale.Version, Score: 80},
		{ID: excluded.ID, Version: excluded.Version, Score: 70},
		{ID: first.ID, Version: first.Version + 1, Score: 60},
		{ID: first.ID, Version: first.Version, Score: 5},
		{ID: first.ID, Version: first.Version, Score: 4},
		{ID: second.ID, Score: 2},
	}
	retriever := retrieverFunc(func(_ context.Context, query memory.Query) ([]memory.Candidate, error) {
		if query.Text != "query" || !slices.Equal(query.Scopes, []string{"selected"}) || !slices.Equal(query.IDs, ids) || query.Limit != 12 {
			t.Fatalf("retriever query = %+v", query)
		}

		return candidates[:min(query.Limit, len(candidates))], nil
	})
	search := newPipeline(t, store, pipeline.WithRetrievers(retriever), pipeline.WithCandidateLimit(12))

	results, err := search.Search(ctx, memory.NewQuery("query", memory.WithScopes("selected"), memory.WithIDs(ids...), memory.WithLimit(2)))
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 || results[0].Score != 5 || results[1].Score != 2 {
		t.Fatalf("filtered ranked results = %+v", results)
	}

	assertEntry(t, results[0].Entry, first)
	assertEntry(t, results[1].Entry, second)
}

func TestPipelineSearchSeparatesCandidateAndResultLimits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	var candidates []memory.Candidate
	for range 12 {
		entry, err := owner.Create(ctx, "personal", "note")
		if err != nil {
			t.Fatal(err)
		}

		candidates = append(candidates, memory.Candidate{ID: entry.ID, Version: entry.Version})
	}

	for _, test := range []struct {
		name           string
		resultLimit    int
		candidateLimit int
		requested      int
		returned       int
	}{
		{name: "default results", resultLimit: 0, candidateLimit: 2, requested: 10, returned: 10},
		{name: "larger results", resultLimit: 12, candidateLimit: 2, requested: 12, returned: 12},
		{name: "larger candidates", resultLimit: 2, candidateLimit: 5, requested: 5, returned: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			retriever := retrieverFunc(func(_ context.Context, query memory.Query) ([]memory.Candidate, error) {
				if query.Limit != test.requested {
					t.Fatalf("candidate request limit = %d, want %d", query.Limit, test.requested)
				}

				return candidates[:min(query.Limit, len(candidates))], nil
			})
			search := newPipeline(t, store, pipeline.WithRetrievers(retriever), pipeline.WithCandidateLimit(test.candidateLimit))

			results, err := search.Search(ctx, memory.NewQuery("note", memory.WithLimit(test.resultLimit)))
			if err != nil || len(results) != test.returned {
				t.Fatalf("results = %+v, %v; want %d entries", results, err, test.returned)
			}
		})
	}
}

func TestPipelineSearchPropagatesFailuresWithoutPartialResults(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))

	entry, err := memory.New(store).Create(ctx, "personal", "canonical content")
	if err != nil {
		t.Fatal(err)
	}

	failure := errors.New("retriever unavailable")
	retriever := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: entry.ID}}, failure
	})

	search := newPipeline(t, store, pipeline.WithRetrievers(retriever))
	if results, err := search.Search(ctx, memory.NewQuery("query")); !errors.Is(err, failure) || results != nil {
		t.Fatalf("failed retrieval = %+v, %v", results, err)
	}

	failure = errors.New("store unavailable")
	broken := newFailingReadStore(store, "unreadable", failure)
	retriever = retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: entry.ID}, {ID: "unreadable"}}, nil
	})

	search = newPipeline(t, broken, pipeline.WithRetrievers(retriever))
	if results, err := search.Search(ctx, memory.NewQuery("query")); !errors.Is(err, failure) || results != nil {
		t.Fatalf("failed canonical read returned partial results: %+v, %v", results, err)
	}
}

type retrieverFunc func(context.Context, memory.Query) ([]memory.Candidate, error)

func (fn retrieverFunc) Retrieve(ctx context.Context, query memory.Query) ([]memory.Candidate, error) {
	return fn(ctx, query)
}

type failingReadStore struct {
	memory.Store
	id  string
	err error
}

func newFailingReadStore(store memory.Store, id string, err error) *failingReadStore {
	return &failingReadStore{Store: store, id: id, err: err}
}

func (s *failingReadStore) Get(ctx context.Context, id string) (memory.Entry, error) {
	if id == s.id {
		return memory.Entry{}, s.err
	}

	return s.Store.Get(ctx, id)
}

func assertEntry(t *testing.T, got, want memory.Entry) {
	t.Helper()

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}

	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("entry = %s, want %s", gotJSON, wantJSON)
	}
}

func openJSONStore(t *testing.T, path string) memory.Store {
	t.Helper()

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	return store
}

func newPipeline(t *testing.T, store memory.Store, options ...pipeline.Option) *pipeline.Pipeline {
	t.Helper()

	search, err := pipeline.New(store, options...)
	if err != nil {
		t.Fatal(err)
	}

	return search
}

func TestPipelineResolvesCanonicalStateAfterRetrieval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	original, err := owner.Create(ctx, "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	counted := newCountingStore(store)
	first := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: original.ID, Version: original.Version}}, nil
	})

	var updated memory.Entry

	second := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		if len(counted.reads) != 0 {
			t.Fatal("canonical resolution started before retrieval finished")
		}

		updated, err = owner.Update(ctx, original.ID, original.Version, "current")
		if err != nil {
			t.Fatal(err)
		}

		return []memory.Candidate{{ID: updated.ID, Version: updated.Version, Score: 2}}, nil
	})
	merger := mergerFunc(func(_ context.Context, lists [][]memory.SearchResult) ([]memory.SearchResult, error) {
		if len(lists) != 2 || len(lists[0]) != 0 || len(lists[1]) != 1 || lists[1][0].Entry.Version != updated.Version {
			t.Fatalf("canonical version validation = %+v", lists)
		}

		assertEntry(t, lists[1][0].Entry, updated)

		return lists[1], nil
	})
	search := newPipeline(t, counted, pipeline.WithRetrievers(first, second), pipeline.WithMerger(merger))

	results, err := search.Search(ctx, memory.NewQuery("question"))
	if err != nil || len(results) != 1 || counted.reads[original.ID] != 1 {
		t.Fatalf("resolved results = %+v, %v; reads=%v", results, err, counted.reads)
	}

	assertEntry(t, results[0].Entry, updated)
}

func TestPipelineValidatesBeforeMergingAndCachesCanonicalReads(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	original, err := owner.Create(ctx, "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	current, err := owner.Update(ctx, original.ID, original.Version, "current")
	if err != nil {
		t.Fatal(err)
	}

	excluded, err := owner.Create(ctx, "other", "excluded")
	if err != nil {
		t.Fatal(err)
	}

	counted := newCountingStore(store)
	sources := [][]memory.Candidate{
		{{ID: original.ID, Version: original.Version, Score: 1}, {ID: "missing", Version: 1}, {ID: excluded.ID, Version: excluded.Version}, {ID: current.ID, Version: current.Version, Score: 0.5}, {ID: current.ID, Version: current.Version, Score: 0.4}},
		{{ID: "missing", Version: 1}, {ID: current.ID, Version: current.Version, Score: 1000}},
	}

	var retrievers []memory.Retriever
	for _, list := range sources {
		retrievers = append(retrievers, retrieverFunc(func(_ context.Context, query memory.Query) ([]memory.Candidate, error) {
			if query.Text != "question" || query.Limit != pipeline.DefaultCandidateLimit || !slices.Equal(query.Scopes, []string{"personal"}) {
				t.Fatalf("retrieval query = %+v", query)
			}

			return list, nil
		}))
	}

	merger := mergerFunc(func(_ context.Context, lists [][]memory.SearchResult) ([]memory.SearchResult, error) {
		if len(lists) != 2 || len(lists[0]) != 1 || len(lists[1]) != 1 || lists[0][0].Entry.ID != current.ID || lists[0][0].Entry.Version != current.Version || lists[0][0].Score != 0.5 || lists[1][0].Entry.ID != current.ID || lists[1][0].Score != 1000 {
			t.Fatalf("unvalidated source lists reached merger: %+v", lists)
		}

		assertEntry(t, lists[0][0].Entry, current)
		assertEntry(t, lists[1][0].Entry, current)

		return []memory.SearchResult{{Entry: current, Score: 3}}, nil
	})

	search := newPipeline(t, counted, pipeline.WithRetrievers(retrievers...), pipeline.WithMerger(merger))
	if len(counted.reads) != 0 {
		t.Fatal("pipeline construction read storage")
	}

	for attempt := 1; attempt <= 2; attempt++ {
		results, err := search.Search(ctx, memory.NewQuery("question", memory.WithScopes("personal"), memory.WithLimit(1)))
		if err != nil || len(results) != 1 || results[0].Score != 3 {
			t.Fatalf("merged results = %+v, %v", results, err)
		}

		assertEntry(t, results[0].Entry, current)

		for _, id := range []string{current.ID, excluded.ID, "missing"} {
			if counted.reads[id] != attempt {
				t.Fatalf("canonical reads for %q = %d, want %d", id, counted.reads[id], attempt)
			}
		}
	}

	if sources[0][0].Version != original.Version || sources[0][3].Score != 0.5 || sources[1][1].Score != 1000 {
		t.Fatalf("pipeline mutated source candidates: %+v", sources)
	}
}

func TestNewValidatesConfigurationWithoutRunningComponents(t *testing.T) {
	t.Parallel()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	counted := newCountingStore(store)

	retriever := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		t.Fatal("invalid search ran retriever")

		return nil, nil
	})
	for _, options := range [][]pipeline.Option{
		nil,
		{pipeline.WithRetrievers(retriever, retriever)},
		{pipeline.WithRetrievers(retriever), pipeline.WithCandidateLimit(0)},
		{pipeline.WithRetrievers(retriever), pipeline.WithCandidateLimit(-1)},
	} {
		if search, err := pipeline.New(counted, options...); search != nil || err == nil {
			t.Fatalf("invalid pipeline configuration accepted: %v, %v", search, err)
		}
	}

	search := newPipeline(t, counted, pipeline.WithRetrievers(retriever))
	if results, err := search.Search(t.Context(), memory.NewQuery("question", memory.WithLimit(-1))); results != nil || err == nil {
		t.Fatalf("negative result limit accepted: %+v, %v", results, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if results, err := search.Search(ctx, memory.NewQuery("question")); results != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search = %+v, %v", results, err)
	}

	if len(counted.reads) != 0 {
		t.Fatalf("invalid search read storage: %v", counted.reads)
	}
}

func TestPipelineBypassesMergerForSingleRetriever(t *testing.T) {
	t.Parallel()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))

	entry, err := memory.New(store).Create(t.Context(), "personal", "content")
	if err != nil {
		t.Fatal(err)
	}

	retriever := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: entry.ID, Version: entry.Version, Score: -2}}, nil
	})
	search := newPipeline(t, store, pipeline.WithRetrievers(retriever), pipeline.WithMerger(mergerFunc(func(context.Context, [][]memory.SearchResult) ([]memory.SearchResult, error) {
		t.Fatal("single retriever invoked merger")

		return nil, nil
	})))

	results, err := search.Search(t.Context(), memory.Query{Text: "question"})
	if err != nil || len(results) != 1 || results[0].Score != -2 {
		t.Fatalf("single-source results = %+v, %v", results, err)
	}
}

func TestPipelinePropagatesMultipleSourceAndMergerFailures(t *testing.T) {
	t.Parallel()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))

	entry, err := memory.New(store).Create(t.Context(), "personal", "content")
	if err != nil {
		t.Fatal(err)
	}

	healthy := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: entry.ID, Version: entry.Version}}, nil
	})
	failure := errors.New("retrieval unavailable")
	broken := retrieverFunc(func(context.Context, memory.Query) ([]memory.Candidate, error) {
		return []memory.Candidate{{ID: entry.ID}}, failure
	})

	search := newPipeline(t, store, pipeline.WithRetrievers(healthy, broken), pipeline.WithMerger(mergerFunc(func(context.Context, [][]memory.SearchResult) ([]memory.SearchResult, error) {
		t.Fatal("failed source invoked merger")

		return nil, nil
	})))
	if results, err := search.Search(t.Context(), memory.NewQuery("question")); results != nil || !errors.Is(err, failure) {
		t.Fatalf("failed source returned partial results: %+v, %v", results, err)
	}

	search = newPipeline(t, store, pipeline.WithRetrievers(healthy, healthy), pipeline.WithMerger(mergerFunc(func(context.Context, [][]memory.SearchResult) ([]memory.SearchResult, error) {
		return []memory.SearchResult{{Entry: entry}}, failure
	})))
	if results, err := search.Search(t.Context(), memory.NewQuery("question")); results != nil || !errors.Is(err, failure) {
		t.Fatalf("failed merger returned partial results: %+v, %v", results, err)
	}
}

func TestPipelineCombinesBM25AndVectorLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))

	lexical, err := bm25.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	semantic, err := vector.New(ctx, store, inmemory.New(), newSemanticEmbedder())
	if err != nil {
		t.Fatal(err)
	}

	merger, err := pipeline.NewRRF()
	if err != nil {
		t.Fatal(err)
	}

	search := newPipeline(t, store, pipeline.WithRetrievers(lexical, semantic), pipeline.WithMerger(merger))
	memories := memory.New(store, memory.WithPipeline(search),
		memory.WithIndexer(lexical, func(_ context.Context, err error) { t.Error(err) }),
		memory.WithIndexer(semantic, func(_ context.Context, err error) { t.Error(err) }),
	)

	first, err := memories.Create(ctx, "personal", "Go concurrency")
	if err != nil {
		t.Fatal(err)
	}

	second, err := memories.Create(ctx, "personal", "Use short answers")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := memories.Create(ctx, "other", "Other workspace"); err != nil {
		t.Fatal(err)
	}

	results, err := memories.Search(ctx, "Go concise", memory.WithScopes("personal"), memory.WithLimit(2))
	if err != nil || len(results) != 2 || results[0].Entry.ID != first.ID || results[1].Entry.ID != second.ID {
		t.Fatalf("hybrid results = %+v, %v", results, err)
	}

	updated, err := memories.Update(ctx, first.ID, first.Version, "Rust concurrency")
	if err != nil {
		t.Fatal(err)
	}

	results, err = memories.Search(ctx, "Go concise", memory.WithScopes("personal"), memory.WithLimit(2))
	if err != nil || len(results) != 2 || results[0].Entry.ID != second.ID || results[1].Entry.ID != updated.ID || results[1].Entry.Version != updated.Version {
		t.Fatalf("updated hybrid results = %+v, %v", results, err)
	}

	if err := memories.Delete(ctx, second.ID, second.Version); err != nil {
		t.Fatal(err)
	}

	results, err = memories.Search(ctx, "Go concise", memory.WithScopes("personal"))
	if err != nil || len(results) != 1 || results[0].Entry.ID != updated.ID {
		t.Fatalf("deleted hybrid results = %+v, %v", results, err)
	}
}

type mergerFunc func(context.Context, [][]memory.SearchResult) ([]memory.SearchResult, error)

func (fn mergerFunc) Merge(ctx context.Context, lists [][]memory.SearchResult) ([]memory.SearchResult, error) {
	return fn(ctx, lists)
}

type countingStore struct {
	memory.Store
	reads map[string]int
}

func newCountingStore(store memory.Store) *countingStore {
	return &countingStore{Store: store, reads: make(map[string]int)}
}

func (s *countingStore) Get(ctx context.Context, id string) (memory.Entry, error) {
	s.reads[id]++

	return s.Store.Get(ctx, id)
}

type semanticEmbedder struct{}

func newSemanticEmbedder() *semanticEmbedder { return &semanticEmbedder{} }

func (e *semanticEmbedder) Space() string { return "test-space" }

func (e *semanticEmbedder) EmbedDocuments(_ context.Context, texts []string) ([]vector.Embedding, error) {
	embeddings := make([]vector.Embedding, len(texts))
	for position, text := range texts {
		switch text {
		case "Go concurrency", "Rust concurrency":
			embeddings[position] = vector.Embedding{Values: []float32{1, 0}}
		case "Use short answers", "Other workspace":
			embeddings[position] = vector.Embedding{Values: []float32{0, 1}}
		default:
			return nil, fmt.Errorf("unexpected document %q", text)
		}
	}

	return embeddings, nil
}

func (e *semanticEmbedder) EmbedQuery(_ context.Context, text string) (vector.Embedding, error) {
	if text != "Go concise" {
		return vector.Embedding{}, fmt.Errorf("unexpected query %q", text)
	}

	return vector.Embedding{Values: []float32{0, 1}}, nil
}
