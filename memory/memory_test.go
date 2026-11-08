package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/memory"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
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

	if _, err := mem.Update(ctx, cleared.ID, cleared.Version, "invalid", memory.WithMetadata("unsupported", make(chan int))); err == nil {
		t.Fatal("Update() accepted non-JSON metadata")
	}

	loaded, err := mem.Get(ctx, cleared.ID)
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
