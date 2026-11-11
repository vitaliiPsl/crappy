package memory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite" // Register the SQLite test driver.

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
	"github.com/vitaliipsl/crappy/memory/pipeline"
	"github.com/vitaliipsl/crappy/memory/sqlite"
)

type storeOpener func(*testing.T, string) (memory.Store, func())

func TestStoreContract(t *testing.T) {
	t.Parallel()

	for _, backend := range []struct {
		name string
		open storeOpener
	}{
		{name: "json", open: openContractJSON},
		{name: "sqlite", open: openContractSQLite},
	} {
		t.Run(backend.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range []struct {
				name string
				run  func(*testing.T, storeOpener)
			}{
				{name: "persistence", run: testStorePersistence},
				{name: "filters", run: testStoreFilters},
				{name: "conditional writes", run: testStoreConditionalWrites},
				{name: "concurrent writes", run: testStoreConcurrentWrites},
				{name: "metadata ownership", run: testStoreMetadataOwnership},
				{name: "cancellation", run: testStoreCancellation},
				{name: "validation", run: testStoreValidation},
				{name: "memory workflow", run: testStoreMemoryWorkflow},
			} {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					test.run(t, backend.open)
				})
			}
		})
	}
}

func testStorePersistence(t *testing.T, open storeOpener) {
	t.Helper()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "entries ?#.db")
	store, closeStore := open(t, path)
	createdAt := time.Date(2026, 1, 1, 12, 30, 1, 123456789, time.FixedZone("offset", 8*60*60))

	want := memory.Entry{
		ID:        "entry/with-an-opaque-id",
		Scope:     "personal",
		Content:   "original\n多言語",
		Source:    "session:1/message:2",
		Metadata:  map[string]json.RawMessage{"count": json.RawMessage(`9007199254740993`)},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Version:   math.MaxUint64 - 1,
	}
	if err := store.Create(ctx, want); err != nil {
		t.Fatal(err)
	}

	closeStore()
	store, closeStore = open(t, path)

	got, err := store.Get(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, got, want)

	want.Scope = "project:crappy"
	want.Content = "corrected"
	want.UpdatedAt = createdAt.Add(time.Hour)
	want.Version++

	if err := store.Update(ctx, want, want.Version-1); err != nil {
		t.Fatal(err)
	}

	closeStore()
	store, closeStore = open(t, path)

	page, err := store.List(ctx, memory.Filter{Scopes: []string{want.Scope}})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("reopened scope = %+v, %v", page, err)
	}

	assertEntry(t, page.Entries[0], want)

	page, err = store.List(ctx, memory.Filter{Scopes: []string{"personal"}})
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("previous scope = %+v, %v", page, err)
	}

	if err := store.Delete(ctx, want.ID, want.Version); err != nil {
		t.Fatal(err)
	}

	closeStore()

	store, _ = open(t, path)

	if _, err := store.Get(ctx, want.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("deleted entry survived reopening: %v", err)
	}
}

func testStoreFilters(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))
	for _, entry := range []memory.Entry{
		{ID: "d", Scope: "personal", Version: 1},
		{ID: "b", Scope: "personal", Version: 1},
		{ID: "a", Scope: "project:crappy", Version: 1},
		{ID: "c", Scope: "personal", Version: 1},
		{ID: "e", Scope: "project:crappy/child", Version: 1},
		{ID: "x' OR 1=1 --", Scope: "scope' ?", Version: 1},
	} {
		if err := store.Create(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}

	filter := memory.Filter{Limit: 2}

	var ids []string
	for {
		page, err := store.List(t.Context(), filter)
		if err != nil {
			t.Fatal(err)
		}

		for _, entry := range page.Entries {
			ids = append(ids, entry.ID)
		}

		if page.NextID == "" {
			break
		}

		if len(page.Entries) != filter.Limit || page.NextID != page.Entries[len(page.Entries)-1].ID {
			t.Fatalf("invalid page boundary: %+v", page)
		}

		filter.AfterID = page.NextID
	}

	if !slices.Equal(ids, []string{"a", "b", "c", "d", "e", "x' OR 1=1 --"}) {
		t.Fatalf("enumerated IDs = %v", ids)
	}

	for _, test := range []struct {
		filter memory.Filter
		want   []string
		nextID string
	}{
		{filter: memory.Filter{Scopes: []string{"personal"}}, want: []string{"b", "c", "d"}},
		{filter: memory.Filter{Scopes: []string{"project:crappy"}}, want: []string{"a"}},
		{filter: memory.Filter{Scopes: []string{"personal", "personal"}, IDs: []string{"d", "a", "d", "missing"}}, want: []string{"d"}},
		{filter: memory.Filter{IDs: []string{"d", "a", "missing"}, Limit: 1}, want: []string{"a"}, nextID: "a"},
		{filter: memory.Filter{IDs: []string{"d", "a"}, AfterID: "a", Limit: 1}, want: []string{"d"}},
		{filter: memory.Filter{Scopes: []string{"unknown"}}, want: []string{}},
		{filter: memory.Filter{Scopes: []string{"scope' ?"}, IDs: []string{"x' OR 1=1 --"}}, want: []string{"x' OR 1=1 --"}},
		{filter: memory.Filter{AfterID: "z", Limit: math.MaxInt}, want: []string{}},
	} {
		page, err := store.List(t.Context(), test.filter)
		if err != nil {
			t.Fatal(err)
		}

		got := make([]string, len(page.Entries))
		for position, entry := range page.Entries {
			got[position] = entry.ID
		}

		if !slices.Equal(got, test.want) || page.NextID != test.nextID {
			t.Fatalf("List(%+v) = %+v, want IDs %v and next ID %q", test.filter, page, test.want, test.nextID)
		}
	}

	if _, err := store.List(t.Context(), memory.Filter{Limit: -1}); err == nil {
		t.Fatal("negative limit was accepted")
	}
}

func testStoreConditionalWrites(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))

	original := memory.Entry{ID: "entry", Scope: "personal", Content: "original", Version: 3}
	if err := store.Create(t.Context(), original); err != nil {
		t.Fatal(err)
	}

	if err := store.Create(t.Context(), original); !errors.Is(err, memory.ErrAlreadyExists) {
		t.Fatalf("duplicate create = %v", err)
	}

	updated := original
	updated.Content = "updated"
	updated.Version++

	if err := store.Update(t.Context(), updated, 2); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale update = %v", err)
	}

	if err := store.Delete(t.Context(), original.ID, 2); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale deletion = %v", err)
	}

	for _, version := range []uint64{0, 2, 3} {
		invalid := updated
		invalid.Scope = "changed"

		invalid.Version = version
		if err := store.Update(t.Context(), invalid, original.Version); err == nil {
			t.Fatalf("non-increasing version %d was accepted", version)
		}
	}

	got, err := store.Get(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, got, original)

	if err := store.Update(t.Context(), updated, original.Version); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(t.Context(), original.ID, original.Version); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("deletion after update = %v", err)
	}

	if err := store.Delete(t.Context(), updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}

	if err := store.Update(t.Context(), updated, updated.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("update after deletion = %v", err)
	}

	if err := store.Delete(t.Context(), updated.ID, updated.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("repeated deletion = %v", err)
	}
}

func testStoreConcurrentWrites(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))

	original := memory.Entry{ID: "shared", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(t.Context(), original); err != nil {
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

			err := store.Update(t.Context(), candidate, original.Version)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, memory.ErrConflict):
				conflicts.Add(1)
			default:
				t.Errorf("conditional update = %v", err)
			}
		})
	}

	workers.Wait()

	if successes.Load() != 1 || conflicts.Load() != 23 {
		t.Fatalf("successful updates = %d, conflicts = %d", successes.Load(), conflicts.Load())
	}
}

func testStoreMetadataOwnership(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))
	for position, metadata := range []map[string]json.RawMessage{
		nil,
		{},
		{"category": json.RawMessage(`"original"`)},
	} {
		entry := memory.Entry{ID: fmt.Sprint(position), Scope: "personal", Metadata: metadata, Version: 1}
		if err := store.Create(t.Context(), entry); err != nil {
			t.Fatal(err)
		}

		got, err := store.Get(t.Context(), entry.ID)
		if err != nil || (got.Metadata == nil) != (metadata == nil) || len(got.Metadata) != len(metadata) {
			t.Fatalf("metadata round trip = %+v, %v", got, err)
		}
	}

	page, err := store.List(t.Context(), memory.Filter{IDs: []string{"2"}})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("metadata page = %+v, %v", page, err)
	}

	page.Entries[0].Metadata["category"][1] = 'x'

	got, err := store.Get(t.Context(), "2")
	if err != nil || string(got.Metadata["category"]) != `"original"` {
		t.Fatalf("list mutation changed stored metadata: %+v, %v", got, err)
	}

	got.Metadata["category"][1] = 'x'

	got, err = store.Get(t.Context(), "2")
	if err != nil || string(got.Metadata["category"]) != `"original"` {
		t.Fatalf("get mutation changed stored metadata: %+v, %v", got, err)
	}
}

func testStoreCancellation(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))

	original := memory.Entry{ID: "entry", Scope: "personal", Content: "original", Version: 1}
	if err := store.Create(t.Context(), original); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.Create(ctx, memory.Entry{ID: "new"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled create = %v", err)
	}

	if _, err := store.Get(ctx, original.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get = %v", err)
	}

	if err := store.Update(ctx, original, original.Version); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update = %v", err)
	}

	if err := store.Delete(ctx, original.ID, original.Version); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled deletion = %v", err)
	}

	if _, err := store.List(ctx, memory.Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list = %v", err)
	}

	got, err := store.Get(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertEntry(t, got, original)
}

func testStoreValidation(t *testing.T, open storeOpener) {
	t.Helper()

	store, _ := open(t, filepath.Join(t.TempDir(), "entries.db"))
	for _, entry := range []memory.Entry{
		{Version: 1},
		{ID: "invalid"},
		{ID: "invalid", Version: 1, Metadata: map[string]json.RawMessage{"bad": json.RawMessage(`{`)}},
	} {
		if err := store.Create(t.Context(), entry); err == nil {
			t.Fatalf("invalid entry was accepted: %+v", entry)
		}
	}

	page, err := store.List(t.Context(), memory.Filter{})
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("invalid creates changed storage: %+v, %v", page, err)
	}
}

func testStoreMemoryWorkflow(t *testing.T, open storeOpener) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "entries.db")
	store, closeStore := open(t, path)
	index := newBM25Index(t, store)

	search, err := pipeline.New(store, pipeline.WithRetrievers(index))
	if err != nil {
		t.Fatal(err)
	}

	mem := memory.New(store,
		memory.WithPipeline(search),
		memory.WithIndexer(index, func(_ context.Context, err error) {
			t.Error(err)
		}),
	)

	entry, err := mem.Create(t.Context(), "personal", "Use concise explanations", memory.WithMetadata("category", "preference"))
	if err != nil {
		t.Fatal(err)
	}

	entry, err = mem.Update(t.Context(), entry.ID, entry.Version, "Use detailed explanations")
	if err != nil {
		t.Fatal(err)
	}

	closeStore()
	store, closeStore = open(t, path)

	index, err = bm25.New(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}

	search, err = pipeline.New(store, pipeline.WithRetrievers(index))
	if err != nil {
		t.Fatal(err)
	}

	mem = memory.New(store,
		memory.WithPipeline(search),
		memory.WithIndexer(index, func(_ context.Context, err error) {
			t.Error(err)
		}),
	)

	results, err := mem.Search(t.Context(), "detailed", memory.WithScopes("personal"))
	if err != nil || len(results) != 1 {
		t.Fatalf("recall after restart = %+v, %v", results, err)
	}

	assertEntry(t, results[0].Entry, entry)

	if err := mem.Delete(t.Context(), entry.ID, entry.Version); err != nil {
		t.Fatal(err)
	}

	results, err = mem.Search(t.Context(), "detailed")
	if err != nil || len(results) != 0 {
		t.Fatalf("deleted entry was retrieved: %+v, %v", results, err)
	}

	closeStore()

	store, _ = open(t, path)
	if _, err := store.Get(t.Context(), entry.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("deleted memory survived restart: %v", err)
	}
}

func openContractJSON(t *testing.T, path string) (memory.Store, func()) {
	t.Helper()

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	return store, func() {}
}

func openContractSQLite(t *testing.T, path string) (memory.Store, func()) {
	t.Helper()

	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	parameters := url.Values{}
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = parameters.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1)

	closeStore := func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(closeStore)

	store, err := sqlite.New(db)
	if err != nil {
		t.Fatal(err)
	}

	return store, closeStore
}
