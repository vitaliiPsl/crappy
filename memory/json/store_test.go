package json_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/memory"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
)

func TestStorePersistsVersionedEntries(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "memory", "entries.json")
	store := openStore(t, path)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	want := memory.Entry{
		ID: "entry/with-an-opaque-id", Scope: "personal", Content: "original",
		Source: "session:1/message:2", Metadata: map[string]json.RawMessage{"count": json.RawMessage(`9007199254740993`)},
		CreatedAt: createdAt, UpdatedAt: createdAt, Version: 7,
	}
	if err := store.Create(ctx, want); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, path)

	got, err := store.Get(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, got, want)

	want.Scope = "project:crappy"
	want.Content = "corrected"
	want.UpdatedAt = createdAt.Add(time.Hour)
	want.Version++

	if err := store.Update(ctx, want, 7); err != nil {
		t.Fatal(err)
	}

	oldScope, err := store.List(ctx, memory.Filter{Scopes: []string{"personal"}})
	if err != nil || len(oldScope.Entries) != 0 {
		t.Fatalf("previous scope = %+v, error = %v", oldScope, err)
	}

	store = openStore(t, path)

	page, err := store.List(ctx, memory.Filter{Scopes: []string{want.Scope}})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("reopened scope = %+v, error = %v", page, err)
	}

	assertEntry(t, page.Entries[0], want)

	if err := store.Delete(ctx, want.ID, want.Version); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, path)
	if _, err := store.Get(ctx, want.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v, want ErrNotFound", err)
	}

	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 || files[0].Name() != "entries.json" {
		t.Fatalf("memory directory = %+v, error = %v, want one document", files, err)
	}
}

func TestStoreEnumeratesFilteredPages(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "entries.json"))

	for _, entry := range []memory.Entry{
		{ID: "d", Scope: "personal", Version: 1}, {ID: "b", Scope: "personal", Version: 1},
		{ID: "a", Scope: "project:crappy", Version: 1}, {ID: "c", Scope: "personal", Version: 1},
		{ID: "e", Scope: "project:crappy-v2", Version: 1},
	} {
		if err := store.Create(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	filter := memory.Filter{Limit: 2}

	var ids []string
	for {
		page, err := store.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}

		for _, entry := range page.Entries {
			ids = append(ids, entry.ID)
		}

		if page.NextID == "" {
			break
		}

		if page.NextID <= filter.AfterID || len(page.Entries) != filter.Limit {
			t.Fatalf("invalid page boundary: filter = %+v, page = %+v", filter, page)
		}

		filter.AfterID = page.NextID
	}

	if !slices.Equal(ids, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("enumerated IDs = %v", ids)
	}

	for _, test := range []struct {
		filter memory.Filter
		want   []string
	}{
		{filter: memory.Filter{Scopes: []string{"personal"}}, want: []string{"b", "c", "d"}},
		{filter: memory.Filter{Scopes: []string{"project:crappy"}}, want: []string{"a"}},
		{filter: memory.Filter{Scopes: []string{"personal", "personal"}, IDs: []string{"d", "a", "d", "missing"}}, want: []string{"d"}},
		{filter: memory.Filter{IDs: []string{"d", "a", "missing"}}, want: []string{"a", "d"}},
		{filter: memory.Filter{Scopes: []string{"unknown"}}, want: []string{}},
	} {
		page, err := store.List(ctx, test.filter)
		if err != nil {
			t.Fatal(err)
		}

		got := make([]string, len(page.Entries))
		for index, entry := range page.Entries {
			got[index] = entry.ID
		}

		if !slices.Equal(got, test.want) {
			t.Fatalf("List(%+v) IDs = %v, want %v", test.filter, got, test.want)
		}
	}

	if _, err := store.List(ctx, memory.Filter{Limit: -1}); err == nil {
		t.Fatal("List() accepted a negative limit")
	}
}

func TestStoreConcurrentConditionalWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "entries.json")
	store := openStore(t, path)

	original := memory.Entry{ID: "shared", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(ctx, original); err != nil {
		t.Fatal(err)
	}

	var (
		successes atomic.Int32
		conflicts atomic.Int32
		workers   sync.WaitGroup
	)

	for index := range 24 {
		workers.Go(func() {
			candidate := original
			candidate.Content = fmt.Sprintf("writer %d", index)
			candidate.Version++

			err := store.Update(ctx, candidate, original.Version)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, memory.ErrConflict):
				conflicts.Add(1)
			default:
				t.Errorf("Update() error = %v", err)
			}
		})
	}

	workers.Wait()

	if successes.Load() != 1 || conflicts.Load() != 23 {
		t.Fatalf("successful writes = %d, conflicts = %d", successes.Load(), conflicts.Load())
	}

	updated, err := openStore(t, path).Get(ctx, original.ID)
	if err != nil || updated.Version != 2 {
		t.Fatalf("persisted winner = %+v, error = %v", updated, err)
	}

	if err := store.Delete(ctx, original.ID, original.Version); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("Delete(stale) error = %v, want ErrConflict", err)
	}

	if err := store.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}

	if err := store.Update(ctx, updated, updated.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("Update(deleted) error = %v, want ErrNotFound", err)
	}

	if err := store.Delete(ctx, updated.ID, updated.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("Delete(missing) error = %v, want ErrNotFound", err)
	}
}

func TestStoreOwnsMetadata(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "entries.json"))
	metadata := map[string]json.RawMessage{"category": json.RawMessage(`"original"`)}

	entry := memory.Entry{ID: "entry", Scope: "personal", Metadata: metadata, Version: 1}
	if err := store.Create(ctx, entry); err != nil {
		t.Fatal(err)
	}

	metadata["category"][1] = 'x'
	entry.Metadata["category"][1] = 'x'

	for range 2 {
		loaded, err := store.Get(ctx, entry.ID)
		if err != nil || string(loaded.Metadata["category"]) != `"original"` {
			t.Fatalf("stored metadata = %v, error = %v", loaded.Metadata, err)
		}

		loaded.Metadata["category"][1] = 'x'
	}

	page, err := store.List(ctx, memory.Filter{})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("List() = %+v, error = %v", page, err)
	}

	page.Entries[0].Metadata["category"][1] = 'x'

	loaded, err := store.Get(ctx, entry.ID)
	if err != nil || string(loaded.Metadata["category"]) != `"original"` {
		t.Fatalf("list mutation changed stored metadata: %v, error = %v", loaded.Metadata, err)
	}
}

func TestStoreRejectsDuplicateCreates(t *testing.T) {
	t.Parallel()

	store := openStore(t, filepath.Join(t.TempDir(), "entries.json"))

	original := memory.Entry{ID: "entry", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(t.Context(), original); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(t.Context(), original); !errors.Is(err, memory.ErrAlreadyExists) {
		t.Fatalf("Create(duplicate) error = %v, want ErrAlreadyExists", err)
	}

	loaded, err := store.Get(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, original)
}

func TestStoreCanceledOperationsPreserveEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "entries.json")
	store := openStore(t, path)

	original := memory.Entry{ID: "entry", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(t.Context(), original); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.Create(ctx, memory.Entry{ID: "new"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}

	if _, err := store.Get(ctx, original.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context.Canceled", err)
	}

	if err := store.Update(ctx, original, original.Version); !errors.Is(err, context.Canceled) {
		t.Fatalf("Update() error = %v, want context.Canceled", err)
	}

	if err := store.Delete(ctx, original.ID, original.Version); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() error = %v, want context.Canceled", err)
	}

	if _, err := store.List(ctx, memory.Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}

	loaded, err := openStore(t, path).Get(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, original)
}

func TestStorePreservesCorruptDocuments(t *testing.T) {
	t.Parallel()

	for _, data := range []string{`null`, `[]`, `{"unfinished":`, `{"entry":{"id":"another","version":1}}`, `{"entry":{"id":"entry"}}`} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "entries.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := jsonstore.New(path); err == nil {
				t.Fatal("New() accepted a corrupt document")
			}

			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("document = %q, error = %v, want %q", got, err, data)
			}
		})
	}
}

func TestStoreFailedWritesPreserveVersionsAndScopes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "entries.json")
	store := openStore(t, path)

	original := memory.Entry{ID: "entry", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(ctx, original); err != nil {
		t.Fatal(err)
	}

	backup := path + ".backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(ctx, memory.Entry{ID: "new", Scope: "changed", Version: 1}); err == nil {
		t.Fatal("Create() succeeded when the document could not be replaced")
	}

	candidate := original
	candidate.Scope = "changed"
	candidate.Content = "replacement"
	candidate.Version++

	if err := store.Update(ctx, candidate, original.Version); err == nil {
		t.Fatal("Update() succeeded when the document could not be replaced")
	}

	if err := store.Delete(ctx, original.ID, original.Version); err == nil {
		t.Fatal("Delete() succeeded when the document could not be replaced")
	}

	loaded, err := store.Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, original)

	for scope, count := range map[string]int{"personal": 1, "changed": 0} {
		page, err := store.List(ctx, memory.Filter{Scopes: []string{scope}})
		if err != nil || len(page.Entries) != count {
			t.Fatalf("failed writes changed scope %q: %+v, error = %v", scope, page, err)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}

	loaded, err = openStore(t, path).Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, loaded, original)

	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("failed writes left temporary files: %+v, error = %v", files, err)
	}
}

func openStore(t *testing.T, path string) *jsonstore.Store {
	t.Helper()

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	return store
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
