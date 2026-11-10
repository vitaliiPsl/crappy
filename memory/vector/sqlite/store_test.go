package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/vector"
	"github.com/vitaliipsl/crappy/memory/vector/sqlite"
)

func TestStorePersistsRecordsAndSearchesAfterReopening(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "nested", "vectors ?#.db")
	store := newStore(t, path)

	records := []vector.Record{
		{
			ID: "a", Scope: "one", Version: 2, Space: "test-space", Values: []float32{2, 0},
			Metadata: map[string]json.RawMessage{"model": json.RawMessage(`"model-a"`), "provider": json.RawMessage(`{"name":"local"}`)},
		},
		{ID: "b", Scope: "two", Version: 1, Space: "test-space", Values: []float32{1, 0}},
		{ID: "c", Scope: "one", Version: 3, Space: "test-space", Values: []float32{1, 1}},
		{ID: "d", Scope: "one/child", Version: 1, Space: "test-space", Values: []float32{-1, 0}},
		{ID: "e", Scope: "one", Version: 1, Space: "other-space", Values: []float32{1, 0, 0}},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatal(err)
	}

	reader := newStore(t, path)
	for _, expected := range records {
		got, err := reader.Get(ctx, expected.ID)
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatalf("committed record = %+v, %v; want %+v", got, err, expected)
		}
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)

	results, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}})
	if err != nil || len(results) != 4 || results[0].ID != "a" || results[1].ID != "b" || results[2].ID != "c" || results[3].ID != "d" {
		t.Fatalf("persisted cosine ranking = %+v, %v", results, err)
	}

	if results[0].Score != 1 || results[1].Score != 1 || math.Abs(results[2].Score-1/math.Sqrt(2)) > 1e-12 || results[3].Score != -1 {
		t.Fatalf("persisted cosine scores = %+v", results)
	}

	results, err = store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}, Scopes: []string{"one"}, IDs: []string{"b", "c", "d"}, Limit: 1})
	if err != nil || len(results) != 1 || results[0].ID != "c" || results[0].Version != 3 {
		t.Fatalf("filtered ranking = %+v, %v", results, err)
	}

	page, err := store.List(ctx, memory.Filter{Limit: 2})
	if err != nil || len(page.Records) != 2 || page.Records[0].ID != "a" || page.Records[1].ID != "b" || page.NextID != "b" {
		t.Fatalf("first record page = %+v, %v", page, err)
	}

	page, err = store.List(ctx, memory.Filter{AfterID: page.NextID, Scopes: []string{"one"}, IDs: []string{"c", "d"}, Limit: 2})
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != "c" || page.NextID != "" {
		t.Fatalf("filtered record page = %+v, %v", page, err)
	}

	records[0].Values[0] = 9
	records[0].Metadata["model"][1] = 'x'

	for range 2 {
		record, err := store.Get(ctx, "a")
		if err != nil || record.Values[0] != 2 || string(record.Metadata["model"]) != `"model-a"` {
			t.Fatalf("record data ownership = %+v, %v", record, err)
		}

		record.Values[0] = 9
		record.Metadata["model"][1] = 'x'
	}
}

func TestStorePersistsReplacementsAndRemovals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "vectors.db")

	store := newStore(t, path)
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "note", Scope: "old", Version: 1, Space: "old", Values: []float32{1, 0}},
		{ID: "note", Scope: "new", Version: 2, Space: "old", Values: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	expected := vector.Record{ID: "note", Scope: "new", Version: 2, Space: "new", Values: []float32{0, 1, 0}}
	if err := store.Upsert(ctx, []vector.Record{expected}); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)

	record, err := store.Get(ctx, "note")
	if err != nil || !reflect.DeepEqual(record, expected) {
		t.Fatalf("persisted replacement = %+v, %v; want %+v", record, err, expected)
	}

	for _, id := range []string{"note", "note", "missing"} {
		if err := store.Remove(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)
	if _, err := store.Get(ctx, "note"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("removed vector survived reopening: %v", err)
	}

	page, err := store.List(ctx, memory.Filter{})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("removed vector remained listed: %+v, %v", page, err)
	}
}

func TestStorePersistsUnsignedVersions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "vectors.db")

	store := newStore(t, path)
	if err := store.Upsert(t.Context(), []vector.Record{{ID: "note", Version: math.MaxUint64, Space: "space", Values: []float32{1}}}); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)

	record, err := store.Get(t.Context(), "note")
	if err != nil || record.Version != math.MaxUint64 || record.Values[0] != 1 {
		t.Fatalf("unsigned version round trip = %+v, %v", record, err)
	}
}

func TestStoreFiltersBeforeCustomScoring(t *testing.T) {
	t.Parallel()

	calls := 0

	store := newStore(t, filepath.Join(t.TempDir(), "vectors.db"), sqlite.WithSimilarity(func(query, document []float32) float64 {
		calls++

		return float64(query[0])*float64(document[0]) + float64(query[1])*float64(document[1])
	}))
	if err := store.Upsert(t.Context(), []vector.Record{
		{ID: "near", Scope: "one", Version: 1, Space: "space", Values: []float32{1, 0}},
		{ID: "far", Scope: "one", Version: 1, Space: "space", Values: []float32{3, 4}},
		{ID: "other-scope", Scope: "two", Version: 1, Space: "space", Values: []float32{1, 0, 0}},
		{ID: "other-space", Scope: "one", Version: 1, Space: "other", Values: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.Search(t.Context(), vector.Query{Space: "space", Scopes: []string{"one"}, Values: []float32{2, 0}, Limit: 1})
	if err != nil || len(results) != 1 || results[0].ID != "far" || results[0].Score != 6 || calls != 2 {
		t.Fatalf("custom scoring = %+v, %v; calls=%d", results, err, calls)
	}

	results, err = store.Search(t.Context(), vector.Query{Space: "space", Values: []float32{1, 0}})
	if err == nil || results != nil {
		t.Fatalf("dimension mismatch produced partial results: %+v, %v", results, err)
	}
}

func TestStoreRejectsNonFiniteScores(t *testing.T) {
	t.Parallel()

	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		store := newStore(t, filepath.Join(t.TempDir(), "vectors.db"), sqlite.WithSimilarity(func(_, _ []float32) float64 { return score }))
		if err := store.Upsert(t.Context(), []vector.Record{{ID: "note", Version: 1, Space: "space", Values: []float32{1}}}); err != nil {
			t.Fatal(err)
		}

		results, err := store.Search(t.Context(), vector.Query{Space: "space", Values: []float32{1}})
		if err == nil || results != nil {
			t.Fatalf("non-finite score produced results: %+v, %v", results, err)
		}
	}
}

func TestStoreRollsBackFailedBatches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "vectors.db")

	store := newStore(t, path)
	if err := store.Upsert(ctx, []vector.Record{{ID: "existing", Version: 1, Space: "space", Values: []float32{1}}}); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []vector.Record{
		{Version: 1, Space: "space", Values: []float32{1}},
		{ID: "invalid", Space: "space", Values: []float32{1}},
		{ID: "invalid", Version: 1, Values: []float32{1}},
		{ID: "invalid", Version: 1, Space: "space"},
		{ID: "invalid", Version: 1, Space: "space", Values: []float32{0, 0}},
		{ID: "invalid", Version: 1, Space: "space", Values: []float32{float32(math.NaN())}},
		{ID: "invalid", Version: 1, Space: "space", Values: []float32{float32(math.Inf(1))}},
		{ID: "invalid", Version: 1, Space: "space", Values: []float32{1}, Metadata: map[string]json.RawMessage{"bad": json.RawMessage(`{`)}},
	} {
		if err := store.Upsert(ctx, []vector.Record{
			{ID: "existing", Version: 2, Space: "space", Values: []float32{2}},
			{ID: "new", Version: 1, Space: "space", Values: []float32{1}},
			invalid,
		}); err == nil {
			t.Fatalf("invalid record accepted: %+v", invalid)
		}
	}

	db := openDatabase(t, path)
	if _, err := db.Exec(`CREATE TRIGGER fail_insert BEFORE INSERT ON vectors WHEN NEW.id = 'failure' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := store.Upsert(ctx, []vector.Record{
		{ID: "existing", Version: 2, Space: "space", Values: []float32{2}},
		{ID: "failure", Version: 1, Space: "space", Values: []float32{1}},
	}); err == nil {
		t.Fatal("database failure was ignored")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)

	page, err := store.List(ctx, memory.Filter{})
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != "existing" || page.Records[0].Version != 1 || page.Records[0].Values[0] != 1 {
		t.Fatalf("failed batch committed partial writes: %+v, %v", page, err)
	}
}

func TestStorePropagatesRemovalFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "vectors.db")
	store := newStore(t, path)

	expected := vector.Record{ID: "note", Version: 1, Space: "space", Values: []float32{1}}
	if err := store.Upsert(ctx, []vector.Record{expected}); err != nil {
		t.Fatal(err)
	}

	db := openDatabase(t, path)
	if _, err := db.Exec(`CREATE TRIGGER fail_remove BEFORE DELETE ON vectors BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := store.Remove(ctx, "note"); err == nil {
		t.Fatal("database failure was ignored")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = newStore(t, path)

	record, err := store.Get(ctx, "note")
	if err != nil || !reflect.DeepEqual(record, expected) {
		t.Fatalf("failed removal changed stored data: %+v, %v", record, err)
	}
}

func TestStoreHonorsCancellation(t *testing.T) {
	t.Parallel()

	store := newStore(t, filepath.Join(t.TempDir(), "vectors.db"))
	if err := store.Upsert(t.Context(), []vector.Record{{ID: "note", Version: 1, Space: "space", Values: []float32{1}}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 2, Space: "space", Values: []float32{2}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upsert = %v", err)
	}

	if err := store.Remove(ctx, "note"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled removal = %v", err)
	}

	if _, err := store.Get(ctx, "note"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get = %v", err)
	}

	if _, err := store.List(ctx, memory.Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list = %v", err)
	}

	if _, err := store.Search(ctx, vector.Query{Space: "space", Values: []float32{1}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search = %v", err)
	}

	record, err := store.Get(t.Context(), "note")
	if err != nil || record.Version != 1 {
		t.Fatalf("canceled writes changed the vector: %+v, %v", record, err)
	}
}

func TestNewRejectsInvalidPathAndDatabase(t *testing.T) {
	t.Parallel()

	corrupt := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"", t.TempDir(), corrupt} {
		if store, err := sqlite.New(path); err == nil || store != nil {
			t.Fatalf("invalid path %q accepted: %v, %v", path, store, err)
		}
	}
}

func newStore(t *testing.T, path string, options ...sqlite.Option) *sqlite.Store {
	t.Helper()

	store, err := sqlite.New(path, options...)
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

func openDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	return db
}
