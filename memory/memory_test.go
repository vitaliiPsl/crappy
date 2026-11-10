package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
	"github.com/vitaliipsl/crappy/memory/pipeline"
)

func TestMemoryEntryLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	mem := memory.New(store)

	created, err := mem.Create(ctx, "personal", "Keep useful comments.\n",
		memory.WithSource("session:original/message:1"),
		memory.WithMetadata("category", "preference"),
		memory.WithMetadata("obsolete", true),
	)
	if err != nil {
		t.Fatal(err)
	}

	if created.ID == "" || created.Version != 1 || created.Scope != "personal" || created.Content != "Keep useful comments.\n" {
		t.Fatalf("created entry = %+v", created)
	}

	if created.CreatedAt.IsZero() || created.CreatedAt.Location() != time.UTC || created.UpdatedAt != created.CreatedAt {
		t.Fatalf("entry timestamps = %+v", created)
	}

	updated, err := mem.Update(ctx, created.ID, created.Version, "Explain intent.",
		memory.WithSource("session:correction/message:2"),
		memory.WithMetadata("category", "convention"),
		memory.WithoutMetadata("obsolete"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if updated.ID != created.ID || updated.Scope != created.Scope || updated.CreatedAt != created.CreatedAt || updated.Version != 2 {
		t.Fatalf("updated identity or version = %+v", updated)
	}

	if updated.Source != "session:correction/message:2" || string(updated.Metadata["category"]) != `"convention"` || len(updated.Metadata) != 1 {
		t.Fatalf("updated source and metadata = %+v", updated)
	}

	if updated.Content != "Explain intent." || updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("updated entry = %+v", updated)
	}

	loaded, err := mem.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, updated)

	if err := mem.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}

	if _, err := mem.Get(ctx, updated.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v, want ErrNotFound", err)
	}

	if err := mem.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatalf("Delete(deleted) error = %v", err)
	}
}

func TestMemoryRejectsStaleUpdatesAndDeletes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	first := memory.New(store)
	second := memory.New(store)

	created, err := first.Create(ctx, "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := second.Update(ctx, created.ID, created.Version, "newer")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := first.Update(ctx, created.ID, created.Version, "stale"); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("Update(stale) error = %v, want ErrConflict", err)
	}

	if err := first.Delete(ctx, created.ID, created.Version); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("Delete(stale) error = %v, want ErrConflict", err)
	}

	loaded, err := first.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, updated)
}

func TestMemoryScopesAreFlatNamespaces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	writer := memory.New(store)

	entries := make(map[string]memory.Entry)
	for _, scope := range []string{"personal", "project:crappy", "project:crappy/child"} {
		entry, err := writer.Create(ctx, scope, "knowledge")
		if err != nil {
			t.Fatal(err)
		}

		entries[scope] = entry
	}

	reader := memory.New(store)

	page, err := reader.List(ctx, memory.Filter{})
	if err != nil || len(page.Entries) != 3 {
		t.Fatalf("List() = %+v, error = %v, want all entries", page, err)
	}

	page, err = reader.List(ctx, memory.Filter{Scopes: []string{"project:crappy"}})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("List(project:crappy) = %+v, error = %v, want only the exact scope", page, err)
	}

	assertEntry(t, page.Entries[0], entries["project:crappy"])

	loaded, err := reader.Get(ctx, entries["personal"].ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, entries["personal"])
}

func TestMemoryMetadataOptionsPreserveUnchangedFields(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := memory.New(openJSONStore(t, filepath.Join(t.TempDir(), "entries.json")))

	created, err := mem.Create(ctx, "personal", "original",
		memory.WithSource("opaque:reference"),
		memory.WithMetadata("settings", map[string]any{"count": 9007199254740993, "tags": []string{"one", "two"}}),
		memory.WithMetadata("nullable", nil),
	)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := mem.Update(ctx, created.ID, created.Version, "corrected")
	if err != nil {
		t.Fatal(err)
	}

	if updated.Source != created.Source || string(updated.Metadata["settings"]) != string(created.Metadata["settings"]) || string(updated.Metadata["nullable"]) != "null" {
		t.Fatalf("update changed untouched source or metadata: %+v", updated)
	}

	if _, err := mem.Update(ctx, updated.ID, updated.Version, "invalid",
		memory.WithSource("unsaved"),
		memory.WithMetadata("settings", "unsaved"),
		memory.WithoutMetadata("nullable"),
		memory.WithMetadata("unsupported", make(chan int)),
	); err == nil {
		t.Fatal("Update() accepted non-JSON metadata")
	}

	loaded, err := mem.Get(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, updated)

	cleared, err := mem.Update(ctx, updated.ID, updated.Version, "corrected",
		memory.WithSource(""), memory.WithoutMetadata("settings"), memory.WithoutMetadata("nullable"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if cleared.Source != "" || len(cleared.Metadata) != 0 {
		t.Fatalf("cleared entry = %+v", cleared)
	}

	if _, err := mem.Create(ctx, "personal", "invalid", memory.WithMetadata("unsupported", make(chan int))); err == nil {
		t.Fatal("Create() accepted non-JSON metadata")
	}

	loaded, err = mem.Get(ctx, cleared.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, cleared)
}

func TestMemoryReturnsBackendErrors(t *testing.T) {
	t.Parallel()

	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	created, err := owner.Create(t.Context(), "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	failure := errors.New("storage unavailable")

	mem := memory.New(newFailingStore(store, failure))
	if entry, err := mem.Create(t.Context(), "personal", "new"); !errors.Is(err, failure) || entry.ID != "" {
		t.Fatalf("Create() = %+v, %v, want storage error without entry", entry, err)
	}

	if entry, err := mem.Update(t.Context(), created.ID, created.Version, "replacement"); !errors.Is(err, failure) || entry.ID != "" {
		t.Fatalf("Update() = %+v, %v, want storage error without entry", entry, err)
	}

	loaded, err := owner.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, created)
}

type failingStore struct {
	memory.Store
	err error
}

func newFailingStore(store memory.Store, err error) *failingStore {
	return &failingStore{Store: store, err: err}
}

func (s *failingStore) Create(context.Context, memory.Entry) error {
	return s.err
}

func (s *failingStore) Update(context.Context, memory.Entry, uint64) error {
	return s.err
}

func (s *failingStore) Delete(context.Context, string, uint64) error {
	return s.err
}

func TestMemoryRejectsEmptyContent(t *testing.T) {
	t.Parallel()

	mem := memory.New(openJSONStore(t, filepath.Join(t.TempDir(), "entries.json")))
	if _, err := mem.Create(t.Context(), "personal", " \n "); err == nil {
		t.Fatal("Create() accepted empty content")
	}

	created, err := mem.Create(t.Context(), "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mem.Update(t.Context(), created.ID, created.Version, ""); err == nil {
		t.Fatal("Update() accepted empty content")
	}

	if _, err := mem.Update(t.Context(), created.ID, 0, "replacement"); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("Update(no version) error = %v, want ErrConflict", err)
	}
}

func TestMemorySearchTracksEntryLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	index := newBM25Index(t, store)

	search, err := pipeline.New(store, pipeline.WithRetrievers(index))
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.New(store,
		memory.WithIndexer(index, func(_ context.Context, err error) { t.Error(err) }),
		memory.WithPipeline(search),
	)

	created, err := mem.Create(ctx, "personal", "Use Go for tools")
	if err != nil {
		t.Fatal(err)
	}

	results, err := mem.Search(ctx, "Go", memory.WithScopes("personal"))
	if err != nil || len(results) != 1 || results[0].Entry.ID != created.ID || results[0].Entry.Version != created.Version || results[0].Score <= 0 {
		t.Fatalf("created entry was not found: %+v, %v", results, err)
	}

	assertEntry(t, results[0].Entry, created)

	updated, err := mem.Update(ctx, created.ID, created.Version, "Use Rust for tools")
	if err != nil {
		t.Fatal(err)
	}

	results, err = mem.Search(ctx, "Go")
	if err != nil || len(results) != 0 {
		t.Fatalf("old terms remained searchable: %+v, %v", results, err)
	}

	results, err = mem.Search(ctx, "Rust")
	if err != nil || len(results) != 1 || results[0].Entry.ID != updated.ID || results[0].Entry.Version != updated.Version {
		t.Fatalf("corrected entry was not found: %+v, %v", results, err)
	}

	assertEntry(t, results[0].Entry, updated)

	if err := mem.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}

	results, err = mem.Search(ctx, "tools")
	if err != nil || len(results) != 0 {
		t.Fatalf("deleted entry remained searchable: %+v, %v", results, err)
	}
}

func TestMemorySearchRequiresPipeline(t *testing.T) {
	t.Parallel()

	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	if results, err := memory.New(store).Search(t.Context(), "query"); !errors.Is(err, memory.ErrPipelineRequired) || results != nil {
		t.Fatalf("unconfigured search = %+v, %v", results, err)
	}
}

func TestMemorySearchDelegatesToPipeline(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	want := []memory.SearchResult{{Entry: memory.Entry{ID: "entry", Content: "canonical"}, Score: 3}}
	search := pipelineFunc(func(received context.Context, query memory.Query) ([]memory.SearchResult, error) {
		if received != ctx || query.Text != "question" || query.Limit != 2 || !slices.Equal(query.Scopes, []string{"personal"}) || !slices.Equal(query.IDs, []string{"entry"}) {
			t.Fatalf("pipeline request = %+v", query)
		}

		return want, nil
	})
	mem := memory.New(store, memory.WithPipeline(search))

	results, err := mem.Search(ctx, "question", memory.WithScopes("personal"), memory.WithIDs("entry"), memory.WithLimit(2))
	if err != nil || len(results) != 1 || results[0].Entry.ID != "entry" || results[0].Score != 3 {
		t.Fatalf("pipeline results = %+v, %v", results, err)
	}

	failure := errors.New("pipeline unavailable")

	mem = memory.New(store, memory.WithPipeline(pipelineFunc(func(context.Context, memory.Query) ([]memory.SearchResult, error) {
		return nil, failure
	})))
	if results, err := mem.Search(ctx, "question"); !errors.Is(err, failure) || results != nil {
		t.Fatalf("pipeline failure = %+v, %v", results, err)
	}
}

type pipelineFunc func(context.Context, memory.Query) ([]memory.SearchResult, error)

func (fn pipelineFunc) Search(ctx context.Context, query memory.Query) ([]memory.SearchResult, error) {
	return fn(ctx, query)
}

func TestMemoryReportsIndexFailuresWithoutFailingCanonicalWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	failure := errors.New("index unavailable")

	var failures []error

	broken := newTestIndexer(
		func(ctx context.Context, entry memory.Entry) error {
			loaded, err := store.Get(ctx, entry.ID)
			if err != nil || loaded.Version != entry.Version || loaded.Content != entry.Content {
				t.Fatalf("indexing ran before persistence: %+v, %v", loaded, err)
			}

			return failure
		},
		func(ctx context.Context, id string) error {
			if _, err := store.Get(ctx, id); !errors.Is(err, memory.ErrNotFound) {
				t.Fatalf("index removal ran before deletion: %v", err)
			}

			return failure
		},
	)
	healthy := newBM25Index(t, store)
	mem := memory.New(store,
		memory.WithIndexer(broken, func(_ context.Context, err error) { failures = append(failures, err) }),
		memory.WithIndexer(healthy, func(_ context.Context, err error) { t.Error(err) }),
	)

	created, err := mem.Create(ctx, "personal", "original")
	if err != nil {
		t.Fatalf("index failure changed create result: %v", err)
	}

	updated, err := mem.Update(ctx, created.ID, created.Version, "replacement")
	if err != nil {
		t.Fatalf("index failure changed update result: %v", err)
	}

	loaded, err := mem.Get(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, updated)

	candidates, err := healthy.Retrieve(ctx, memory.NewQuery("replacement"))
	if err != nil || len(candidates) != 1 || candidates[0].Version != updated.Version {
		t.Fatalf("a failing index prevented another from updating: %+v, %v", candidates, err)
	}

	if err := mem.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatalf("index failure changed delete result: %v", err)
	}

	candidates, err = healthy.Retrieve(ctx, memory.NewQuery("replacement"))
	if err != nil || len(candidates) != 0 {
		t.Fatalf("a failing index prevented another from removing: %+v, %v", candidates, err)
	}

	if len(failures) != 3 {
		t.Fatalf("index failures = %v, want one for each committed mutation", failures)
	}

	for _, err := range failures {
		if !errors.Is(err, failure) {
			t.Fatalf("index failure lost its cause: %v", err)
		}
	}
}

func TestMemoryDoesNotIndexFailedCanonicalWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	owner := memory.New(store)

	created, err := owner.Create(ctx, "personal", "original")
	if err != nil {
		t.Fatal(err)
	}

	notifications := 0
	index := newTestIndexer(
		func(context.Context, memory.Entry) error {
			notifications++

			return nil
		},
		func(context.Context, string) error {
			notifications++

			return nil
		},
	)
	option := memory.WithIndexer(index, func(_ context.Context, err error) { t.Error(err) })
	failure := errors.New("store unavailable")
	broken := memory.New(newFailingStore(store, failure), option)

	if _, err := broken.Create(ctx, "personal", "new"); !errors.Is(err, failure) {
		t.Fatalf("create error = %v", err)
	}

	if _, err := broken.Update(ctx, created.ID, created.Version, "changed"); !errors.Is(err, failure) {
		t.Fatalf("update error = %v", err)
	}

	if err := broken.Delete(ctx, created.ID, created.Version); !errors.Is(err, failure) {
		t.Fatalf("delete error = %v", err)
	}

	mem := memory.New(store, option)
	if _, err := mem.Update(ctx, created.ID, 0, "stale"); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	if err := mem.Delete(ctx, created.ID, 0); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale delete error = %v", err)
	}

	if notifications != 0 {
		t.Fatalf("failed writes triggered %d index notifications", notifications)
	}
}

func TestMemoryRepeatedDeleteRetriesIndexRemoval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openJSONStore(t, filepath.Join(t.TempDir(), "entries.json"))
	index := newBM25Index(t, store)
	failure := errors.New("temporary removal failure")
	attempts := 0
	retrying := newTestIndexer(index.Upsert, func(ctx context.Context, id string) error {
		attempts++
		if attempts == 1 {
			return failure
		}

		return index.Remove(ctx, id)
	})
	mem := memory.New(store,
		memory.WithIndexer(retrying, func(_ context.Context, err error) {
			if !errors.Is(err, failure) {
				t.Error(err)
			}
		}),
	)

	created, err := mem.Create(ctx, "personal", "remembered")
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := mem.Delete(ctx, created.ID, created.Version); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := index.Retrieve(ctx, memory.NewQuery("remembered"))
	if err != nil || len(candidates) != 0 || attempts != 2 {
		t.Fatalf("repeated delete did not repair index removal: %+v, %v, attempts=%d", candidates, err, attempts)
	}
}

type testIndexer struct {
	upsert func(context.Context, memory.Entry) error
	remove func(context.Context, string) error
}

func newTestIndexer(upsert func(context.Context, memory.Entry) error, remove func(context.Context, string) error) *testIndexer {
	return &testIndexer{upsert: upsert, remove: remove}
}

func (i *testIndexer) Upsert(ctx context.Context, entry memory.Entry) error {
	return i.upsert(ctx, entry)
}

func (i *testIndexer) Remove(ctx context.Context, id string) error {
	return i.remove(ctx, id)
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

func newBM25Index(t *testing.T, store memory.Store) *bm25.Index {
	t.Helper()

	index, err := bm25.New(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}

	return index
}
