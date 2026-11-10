package vector_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
	"github.com/vitaliipsl/crappy/memory/pipeline"
	"github.com/vitaliipsl/crappy/memory/vector"
	"github.com/vitaliipsl/crappy/memory/vector/inmemory"
	sqlitestore "github.com/vitaliipsl/crappy/memory/vector/sqlite"
)

func TestNewReconcilesVectorsAndReusesCurrentEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	entries := newEntryStore(t)
	for _, entry := range []memory.Entry{
		{ID: "a", Scope: "personal", Content: "unchanged", Version: 1},
		{ID: "b", Scope: "project", Content: "corrected", Version: 2},
		{ID: "c", Scope: "personal", Content: "missing", Version: 1},
	} {
		if err := entries.Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "a", Scope: "personal", Version: 1, Space: "test-space", Values: []float32{1, 0}},
		{ID: "b", Scope: "personal", Version: 1, Space: "test-space", Values: []float32{1, 0}},
		{ID: "orphan", Scope: "personal", Version: 5, Space: "test-space", Values: []float32{1, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	documentCalls, queryCalls := 0, 0

	embedder := newEmbedder("test-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		documentCalls++

		if !slices.Equal(texts, []string{"corrected", "missing"}) {
			t.Fatalf("document embedding input = %v", texts)
		}

		return []vector.Embedding{
			{Values: []float32{0, 1}, Metadata: map[string]json.RawMessage{"tokens": json.RawMessage(`2`)}},
			{Values: []float32{-1, 0}, Metadata: map[string]json.RawMessage{"tokens": json.RawMessage(`1`)}},
		}, nil
	}, func(_ context.Context, text string) (vector.Embedding, error) {
		queryCalls++

		if text != "semantic query" {
			t.Fatalf("query embedding input = %q", text)
		}

		return vector.Embedding{Values: []float32{0, 1}}, nil
	})
	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	index, err := vector.New(ctx, entries, store, embedder)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(ctx, "orphan"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("orphaned vector was retained: %v", err)
	}

	for id, tokens := range map[string]string{"b": "2", "c": "1"} {
		record, err := store.Get(ctx, id)
		if err != nil || string(record.Metadata["tokens"]) != tokens {
			t.Fatalf("embedding metadata for %q = %+v, %v", id, record, err)
		}
	}

	search, err := pipeline.New(entries, pipeline.WithRetrievers(index))
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.New(entries, memory.WithPipeline(search))

	results, err := mem.Search(ctx, "semantic query", memory.WithScopes("project"), memory.WithIDs("b"), memory.WithLimit(1))
	if err != nil || len(results) != 1 || results[0].Entry.ID != "b" || results[0].Entry.Version != 2 || results[0].Entry.Content != "corrected" || results[0].Score != 1 {
		t.Fatalf("canonical semantic results = %+v, %v", results, err)
	}

	if documentCalls != 1 || queryCalls != 1 {
		t.Fatalf("document calls=%d, query calls=%d; want one each", documentCalls, queryCalls)
	}
}

func TestNewRefreshesVectorsWhenEmbeddingSpaceChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	entries := newEntryStore(t)
	if err := entries.Create(ctx, memory.Entry{ID: "note", Scope: "personal", Content: "saved note", Version: 3}); err != nil {
		t.Fatal(err)
	}

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{{
		ID: "note", Scope: "personal", Version: 3, Space: "old-space", Values: []float32{1, 0},
		Metadata: map[string]json.RawMessage{"model": json.RawMessage(`"embedding-model"`), "revision": json.RawMessage(`"v1"`)},
	}}); err != nil {
		t.Fatal(err)
	}

	documentCalls := 0
	revision := json.RawMessage(`"v2"`)

	embedder := newEmbedder("new-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		documentCalls++

		if !slices.Equal(texts, []string{"saved note"}) {
			t.Fatalf("space refresh input = %v", texts)
		}

		return []vector.Embedding{{
			Values:   []float32{0, 1, 0},
			Metadata: map[string]json.RawMessage{"model": json.RawMessage(`"embedding-model"`), "revision": revision},
		}}, nil
	}, func(context.Context, string) (vector.Embedding, error) {
		return vector.Embedding{Values: []float32{0, 1, 0}}, nil
	})
	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	revision = json.RawMessage(`"v3"`)

	index, err := vector.New(ctx, entries, store, embedder)
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.Get(ctx, "note")
	if err != nil || record.Space != "new-space" || record.Version != 3 || !slices.Equal(record.Values, []float32{0, 1, 0}) || documentCalls != 1 {
		t.Fatalf("space refresh = %+v, %v; document calls=%d", record, err, documentCalls)
	}

	if string(record.Metadata["model"]) != `"embedding-model"` || string(record.Metadata["revision"]) != `"v2"` {
		t.Fatalf("stored metadata does not describe the generated vector: %+v", record.Metadata)
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("query"))
	if err != nil || len(candidates) != 1 || candidates[0].ID != "note" || candidates[0].Score != 1 {
		t.Fatalf("retrieval after space refresh = %+v, %v", candidates, err)
	}

	canonical, err := entries.Get(ctx, "note")
	if err != nil || canonical.Version != 3 {
		t.Fatalf("space refresh changed canonical version: %+v, %v", canonical, err)
	}
}

func TestNewRequiresEmbeddingSpace(t *testing.T) {
	t.Parallel()

	source := newPagedEntryStore(func(context.Context, memory.Filter) (memory.Page, error) {
		t.Fatal("missing space caused canonical reads")

		return memory.Page{}, nil
	})

	embedder := newEmbedder("", func(context.Context, []string) ([]vector.Embedding, error) {
		t.Fatal("missing space caused embedding")

		return nil, nil
	}, nil)
	if index, err := vector.New(t.Context(), source, inmemory.New(), embedder); index != nil || err == nil {
		t.Fatalf("missing space accepted: %v, %v", index, err)
	}
}

func TestNewBatchesCanonicalPages(t *testing.T) {
	t.Parallel()

	var entries []memory.Entry
	for position := range memory.DefaultPageSize + 1 {
		entries = append(entries, memory.Entry{ID: fmt.Sprintf("%03d", position), Content: "note", Version: 1})
	}

	loads := 0
	source := newPagedEntryStore(func(_ context.Context, filter memory.Filter) (memory.Page, error) {
		loads++

		if loads > 2 || filter.Limit != memory.DefaultPageSize {
			t.Fatalf("unexpected page request %d: %+v", loads, filter)
		}

		if filter.AfterID == "" {
			if loads != 1 {
				t.Fatal("initialization restarted pagination")
			}

			return memory.Page{Entries: entries[:memory.DefaultPageSize], NextID: entries[memory.DefaultPageSize-1].ID}, nil
		}

		if filter.AfterID != entries[memory.DefaultPageSize-1].ID {
			t.Fatalf("unexpected page cursor %q", filter.AfterID)
		}

		return memory.Page{Entries: entries[memory.DefaultPageSize:]}, nil
	})

	var batches []int

	embedder := newEmbedder("test-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		batches = append(batches, len(texts))

		embeddings := make([]vector.Embedding, len(texts))
		for position := range embeddings {
			embeddings[position] = vector.Embedding{Values: []float32{1, 0}}
		}

		return embeddings, nil
	}, nil)

	store := inmemory.New()
	if _, err := vector.New(t.Context(), source, store, embedder); err != nil {
		t.Fatal(err)
	}

	page, err := store.List(t.Context(), memory.Filter{Limit: len(entries)})
	if err != nil || len(page.Records) != len(entries) || loads != 2 || !slices.Equal(batches, []int{memory.DefaultPageSize, 1}) {
		t.Fatalf("initialization records=%d, loads=%d, batches=%v, error=%v", len(page.Records), loads, batches, err)
	}
}

func TestIndexTracksCanonicalLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	entries := newEntryStore(t)
	store := inmemory.New()
	embedder := newEmbedder("test-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		if len(texts) != 1 {
			t.Fatalf("runtime batch = %v", texts)
		}

		switch texts[0] {
		case "Prefer brief answers":
			return []vector.Embedding{{Values: []float32{1, 0}}}, nil
		case "Prefer detailed answers":
			return []vector.Embedding{{Values: []float32{0, 1}}}, nil
		default:
			t.Fatalf("unexpected document embedding input: %v", texts)

			return nil, nil
		}
	}, func(_ context.Context, text string) (vector.Embedding, error) {
		switch text {
		case "concise responses":
			return vector.Embedding{Values: []float32{1, 0}}, nil
		case "long explanations":
			return vector.Embedding{Values: []float32{0, 1}}, nil
		default:
			t.Fatalf("unexpected query embedding input: %q", text)

			return vector.Embedding{}, nil
		}
	})

	index, err := vector.New(ctx, entries, store, embedder)
	if err != nil {
		t.Fatal(err)
	}

	search, err := pipeline.New(entries, pipeline.WithRetrievers(index))
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.New(entries,
		memory.WithIndexer(index, func(_ context.Context, err error) { t.Error(err) }),
		memory.WithPipeline(search),
	)

	created, err := mem.Create(ctx, "personal", "Prefer brief answers")
	if err != nil {
		t.Fatal(err)
	}

	results, err := mem.Search(ctx, "concise responses")
	if err != nil || len(results) != 1 || results[0].Entry.ID != created.ID || results[0].Score != 1 {
		t.Fatalf("semantic recall = %+v, %v", results, err)
	}

	updated, err := mem.Update(ctx, created.ID, created.Version, "Prefer detailed answers")
	if err != nil {
		t.Fatal(err)
	}

	results, err = mem.Search(ctx, "long explanations")
	if err != nil || len(results) != 1 || results[0].Entry.Version != updated.Version || results[0].Score != 1 {
		t.Fatalf("updated semantic recall = %+v, %v", results, err)
	}

	if err := mem.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}

	results, err = mem.Search(ctx, "long explanations")
	if err != nil || len(results) != 0 {
		t.Fatalf("deleted semantic memory = %+v, %v", results, err)
	}
}

func TestIndexPropagatesEmbeddingFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	entries := newEntryStore(t)

	store := inmemory.New()

	if err := entries.Create(ctx, memory.Entry{ID: "note", Content: "note", Version: 1}); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("embedding unavailable")

	embedder := newEmbedder("test-space", func(context.Context, []string) ([]vector.Embedding, error) {
		return nil, failure
	}, nil)
	if index, err := vector.New(ctx, entries, store, embedder); index != nil || !errors.Is(err, failure) {
		t.Fatalf("failed initialization = %v, %v", index, err)
	}

	embedder = newEmbedder("test-space", func(context.Context, []string) ([]vector.Embedding, error) { return nil, nil }, nil)
	if index, err := vector.New(ctx, entries, store, embedder); index != nil || err == nil {
		t.Fatalf("misaligned embeddings were accepted: %v, %v", index, err)
	}

	if _, err := store.Get(ctx, "note"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("failed embedding wrote a vector: %v", err)
	}

	embedder = newEmbedder("test-space", func(context.Context, []string) ([]vector.Embedding, error) {
		return []vector.Embedding{{Values: []float32{0, 0}}}, nil
	}, nil)
	if index, err := vector.New(ctx, entries, store, embedder); index != nil || err == nil {
		t.Fatalf("zero vector was accepted: %v, %v", index, err)
	}

	embedder = newEmbedder("test-space", nil, func(context.Context, string) (vector.Embedding, error) {
		return vector.Embedding{}, failure
	})

	index, err := vector.New(ctx, newEntryStore(t), store, embedder)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := index.Retrieve(ctx, memory.NewQuery("query")); !errors.Is(err, failure) {
		t.Fatalf("query embedding failure = %v", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	cancel()

	if index, err := vector.New(ctx, entries, store, embedder); index != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled initialization = %v, %v", index, err)
	}
}

func TestNewPropagatesCanonicalReadErrors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := inmemory.New()
	failure := errors.New("canonical store unavailable")
	source := newPagedEntryStore(func(context.Context, memory.Filter) (memory.Page, error) {
		return memory.Page{}, failure
	})

	embedder := newEmbedder("test-space", func(context.Context, []string) ([]vector.Embedding, error) {
		t.Fatal("invalid initialization attempted embedding")

		return nil, nil
	}, nil)
	if index, err := vector.New(ctx, source, store, embedder); index != nil || !errors.Is(err, failure) {
		t.Fatalf("canonical loading failure = %v, %v", index, err)
	}
}

func TestNewRefreshesAnyVersionMismatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := inmemory.New()

	entries := newEntryStore(t)
	if err := entries.Create(ctx, memory.Entry{ID: "note", Content: "canonical", Version: 1}); err != nil {
		t.Fatal(err)
	}

	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 2, Space: "test-space", Values: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	embedder := newEmbedder("test-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		if !slices.Equal(texts, []string{"canonical"}) {
			t.Fatalf("version mismatch embedded %v", texts)
		}

		return []vector.Embedding{{Values: []float32{0, 1}}}, nil
	}, nil)
	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	record, err := store.Get(ctx, "note")
	if err != nil || record.Version != 1 || !slices.Equal(record.Values, []float32{0, 1}) {
		t.Fatalf("version mismatch was not reconciled: %+v, %v", record, err)
	}
}

func TestNewReconcilesPersistedVectorsAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	directory := t.TempDir()
	entryPath := filepath.Join(directory, "entries.json")
	vectorPath := filepath.Join(directory, "vectors.db")

	entries, err := jsonstore.New(entryPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range []memory.Entry{
		{ID: "a", Scope: "personal", Content: "unchanged", Version: 1},
		{ID: "b", Scope: "personal", Content: "original", Version: 1},
		{ID: "c", Scope: "personal", Content: "deleted later", Version: 1},
	} {
		if err := entries.Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	embedder := newEmbedder("space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		embeddings := make([]vector.Embedding, len(texts))
		for position := range embeddings {
			embeddings[position] = vector.Embedding{Values: []float32{1, 0}}
		}

		return embeddings, nil
	}, nil)

	store := newVectorStore(t, vectorPath)
	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err = jsonstore.New(entryPath)
	if err != nil {
		t.Fatal(err)
	}

	store = newVectorStore(t, vectorPath)
	embedder = newEmbedder("space", func(context.Context, []string) ([]vector.Embedding, error) {
		t.Fatal("unchanged restart requested document embeddings")

		return nil, nil
	}, func(context.Context, string) (vector.Embedding, error) {
		return vector.Embedding{Values: []float32{1, 0}}, nil
	})

	index, err := vector.New(ctx, entries, store, embedder)
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("query"))
	if err != nil || len(candidates) != 3 || candidates[0].ID != "a" || candidates[0].Score != 1 {
		t.Fatalf("restarted search = %+v, %v", candidates, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := entries.Update(ctx, memory.Entry{ID: "b", Scope: "project", Content: "corrected", Version: 2}, 1); err != nil {
		t.Fatal(err)
	}

	if err := entries.Delete(ctx, "c", 1); err != nil {
		t.Fatal(err)
	}

	if err := entries.Create(ctx, memory.Entry{ID: "d", Scope: "personal", Content: "new", Version: 1}); err != nil {
		t.Fatal(err)
	}

	store = newVectorStore(t, vectorPath)
	documentCalls := 0

	embedder = newEmbedder("space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		documentCalls++

		if !slices.Equal(texts, []string{"corrected", "new"}) {
			t.Fatalf("restart reconciliation embedded %v", texts)
		}

		return []vector.Embedding{{Values: []float32{0, 1}}, {Values: []float32{-1, 0}}}, nil
	}, nil)

	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	if documentCalls != 1 {
		t.Fatalf("reconciliation document calls = %d, want 1", documentCalls)
	}

	if _, err := store.Get(ctx, "c"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("restart retained an orphaned vector: %v", err)
	}

	record, err := store.Get(ctx, "b")
	if err != nil || record.Version != 2 || record.Scope != "project" || !slices.Equal(record.Values, []float32{0, 1}) {
		t.Fatalf("restart retained stale vector data: %+v, %v", record, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newVectorStore(t, vectorPath)
	embedder = newEmbedder("new-space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		if !slices.Equal(texts, []string{"unchanged", "corrected", "new"}) {
			t.Fatalf("space replacement embedded %v", texts)
		}

		return []vector.Embedding{{Values: []float32{1, 0, 0}}, {Values: []float32{0, 1, 0}}, {Values: []float32{0, 0, 1}}}, nil
	}, nil)

	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	for id, version := range map[string]uint64{"a": 1, "b": 2, "d": 1} {
		record, err := store.Get(ctx, id)
		if err != nil || record.Space != "new-space" || record.Version != version || len(record.Values) != 3 {
			t.Fatalf("persistent space replacement = %+v, %v", record, err)
		}
	}
}

func TestNewReusesCommittedBatchesAfterInterruptedInitialization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	entries := newEntryStore(t)

	for position := 0; position <= memory.DefaultPageSize; position++ {
		id := fmt.Sprintf("entry-%03d", position)
		if err := entries.Create(ctx, memory.Entry{ID: id, Content: id, Version: 1}); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(t.TempDir(), "vectors.db")
	store := newVectorStore(t, path)
	calls := 0
	wantErr := errors.New("embedding unavailable")

	embedder := newEmbedder("space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		calls++
		if calls == 2 {
			return nil, wantErr
		}

		embeddings := make([]vector.Embedding, len(texts))
		for position := range embeddings {
			embeddings[position] = vector.Embedding{Values: []float32{1}}
		}

		return embeddings, nil
	}, nil)
	if index, err := vector.New(ctx, entries, store, embedder); index != nil || !errors.Is(err, wantErr) {
		t.Fatalf("interrupted initialization = %v, %v", index, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newVectorStore(t, path)
	calls = 0

	embedder = newEmbedder("space", func(_ context.Context, texts []string) ([]vector.Embedding, error) {
		calls++

		if !slices.Equal(texts, []string{fmt.Sprintf("entry-%03d", memory.DefaultPageSize)}) {
			t.Fatalf("retried already committed embeddings: %v", texts)
		}

		return []vector.Embedding{{Values: []float32{1}}}, nil
	}, nil)
	if _, err := vector.New(ctx, entries, store, embedder); err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Fatalf("remaining embedding batches = %d, want 1", calls)
	}
}

func newVectorStore(t *testing.T, path string) *sqlitestore.Store {
	t.Helper()

	store, err := sqlitestore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	return store
}

type testEmbedder struct {
	space     string
	documents func(context.Context, []string) ([]vector.Embedding, error)
	query     func(context.Context, string) (vector.Embedding, error)
}

func newEmbedder(space string, documents func(context.Context, []string) ([]vector.Embedding, error), query func(context.Context, string) (vector.Embedding, error)) *testEmbedder {
	return &testEmbedder{space: space, documents: documents, query: query}
}

func (e *testEmbedder) Space() string {
	return e.space
}

func (e *testEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([]vector.Embedding, error) {
	return e.documents(ctx, texts)
}

func (e *testEmbedder) EmbedQuery(ctx context.Context, text string) (vector.Embedding, error) {
	return e.query(ctx, text)
}

type pagedEntryStore struct {
	memory.Store
	list func(context.Context, memory.Filter) (memory.Page, error)
}

func newPagedEntryStore(list func(context.Context, memory.Filter) (memory.Page, error)) *pagedEntryStore {
	return &pagedEntryStore{list: list}
}

func (s *pagedEntryStore) List(ctx context.Context, filter memory.Filter) (memory.Page, error) {
	return s.list(ctx, filter)
}

func newEntryStore(t *testing.T) memory.Store {
	t.Helper()

	store, err := jsonstore.New(filepath.Join(t.TempDir(), "entries.json"))
	if err != nil {
		t.Fatal(err)
	}

	return store
}
