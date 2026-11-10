package inmemory_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/vector"
	"github.com/vitaliipsl/crappy/memory/vector/inmemory"
)

func TestStoreRanksCosineSimilarityWithFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "a", Scope: "one", Version: 2, Space: "test-space", Values: []float32{2, 0}},
		{ID: "b", Scope: "two", Version: 1, Space: "test-space", Values: []float32{1, 0}},
		{ID: "c", Scope: "one", Version: 3, Space: "test-space", Values: []float32{1, 1}},
		{ID: "d", Scope: "one/child", Version: 1, Space: "test-space", Values: []float32{-1, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}})
	if err != nil || len(candidates) != 4 || candidates[0].ID != "a" || candidates[1].ID != "b" || candidates[2].ID != "c" || candidates[3].ID != "d" {
		t.Fatalf("cosine ordering = %+v, %v", candidates, err)
	}

	if candidates[0].Score != 1 || candidates[1].Score != 1 || math.Abs(candidates[2].Score-1/math.Sqrt(2)) > 1e-12 || candidates[3].Score != -1 {
		t.Fatalf("cosine scores = %+v", candidates)
	}

	candidates, err = store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}, Scopes: []string{"one"}, Limit: 2})
	if err != nil || len(candidates) != 2 || candidates[0].ID != "a" || candidates[1].ID != "c" || candidates[1].Version != 3 {
		t.Fatalf("scoped cosine ranking = %+v, %v", candidates, err)
	}

	candidates, err = store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}, IDs: []string{"c", "d"}, Limit: 1})
	if err != nil || len(candidates) != 1 || candidates[0].ID != "c" {
		t.Fatalf("ID filter before limit = %+v, %v", candidates, err)
	}

	page, err := store.List(ctx, memory.Filter{Limit: 2})
	if err != nil || len(page.Records) != 2 || page.Records[0].ID != "a" || page.Records[1].ID != "b" || page.NextID != "b" {
		t.Fatalf("first record page = %+v, %v", page, err)
	}

	page, err = store.List(ctx, memory.Filter{AfterID: page.NextID, Scopes: []string{"one"}, IDs: []string{"c", "d"}, Limit: 2})
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != "c" || page.NextID != "" {
		t.Fatalf("filtered record page = %+v, %v", page, err)
	}
}

func TestStoreChecksDimensionsBeforeScoring(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	calls := 0

	store := inmemory.New(inmemory.WithSimilarity(func(query, document []float32) float64 {
		if len(query) != len(document) {
			t.Fatalf("similarity received incompatible vectors: %v, %v", query, document)
		}

		calls++

		return inmemory.Cosine(query, document)
	}))
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "two", Scope: "one", Version: 1, Space: "shared", Values: []float32{1, 0}},
		{ID: "three", Scope: "two", Version: 1, Space: "shared", Values: []float32{1, 0, 0}},
		{ID: "four", Scope: "one", Version: 1, Space: "other", Values: []float32{1, 0, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		query  vector.Query
		wantID string
	}{
		{query: vector.Query{Space: "shared", Values: []float32{1, 0}, Scopes: []string{"one"}}, wantID: "two"},
		{query: vector.Query{Space: "shared", Values: []float32{1, 0, 0}, IDs: []string{"three"}}, wantID: "three"},
		{query: vector.Query{Space: "other", Values: []float32{1, 0, 0, 0}}, wantID: "four"},
	} {
		candidates, err := store.Search(ctx, test.query)
		if err != nil || len(candidates) != 1 || candidates[0].ID != test.wantID || candidates[0].Score != 1 {
			t.Fatalf("matching dimensions for %q = %+v, %v", test.wantID, candidates, err)
		}
	}

	for _, values := range [][]float32{{1}, {1, 0, 0}} {
		candidates, err := store.Search(ctx, vector.Query{Space: "shared", Values: values, IDs: []string{"two"}})
		if err == nil || candidates != nil || calls != 3 {
			t.Fatalf("mismatched query %v = %+v, %v; similarity calls=%d", values, candidates, err, calls)
		}
	}

	candidates, err := store.Search(ctx, vector.Query{Space: "shared", Values: []float32{1, 0}})
	if err == nil || candidates != nil {
		t.Fatalf("dimension mismatch produced partial results: %+v, %v", candidates, err)
	}
}

func TestStoreRanksWithCustomSimilarity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		similarity inmemory.Similarity
		wantID     string
		wantScore  float64
	}{
		{
			name: "dot product",
			similarity: func(query, document []float32) float64 {
				return float64(query[0])*float64(document[0]) + float64(query[1])*float64(document[1])
			},
			wantID: "far", wantScore: 6,
		},
		{
			name: "negative Euclidean distance",
			similarity: func(query, document []float32) float64 {
				return -math.Hypot(float64(query[0])-float64(document[0]), float64(query[1])-float64(document[1]))
			},
			wantID: "near", wantScore: -1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			calls := 0

			store := inmemory.New(inmemory.WithSimilarity(func(query, document []float32) float64 {
				calls++

				if !slices.Equal(query, []float32{2, 0}) || document[0] == 10 {
					t.Fatalf("unexpected similarity inputs: %v, %v", query, document)
				}

				return test.similarity(query, document)
			}))
			if err := store.Upsert(ctx, []vector.Record{
				{ID: "near", Scope: "one", Version: 1, Space: "test-space", Values: []float32{1, 0}},
				{ID: "far", Scope: "one", Version: 1, Space: "test-space", Values: []float32{3, 4}},
				{ID: "other-scope", Scope: "two", Version: 1, Space: "test-space", Values: []float32{10, 0}},
				{ID: "other-space", Scope: "one", Version: 1, Space: "old-space", Values: []float32{10, 0}},
			}); err != nil {
				t.Fatal(err)
			}

			candidates, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{2, 0}, Scopes: []string{"one"}, Limit: 1})
			if err != nil || len(candidates) != 1 || candidates[0].ID != test.wantID || candidates[0].Score != test.wantScore || calls != 2 {
				t.Fatalf("custom ranking = %+v, %v; calls=%d", candidates, err, calls)
			}
		})
	}
}

func TestStoreRejectsNonFiniteSimilarityScores(t *testing.T) {
	t.Parallel()

	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		ctx := t.Context()

		store := inmemory.New(inmemory.WithSimilarity(func(_ []float32, document []float32) float64 {
			if document[0] == 1 {
				return 1
			}

			return score
		}))
		if err := store.Upsert(ctx, []vector.Record{
			{ID: "valid", Version: 1, Space: "test-space", Values: []float32{1, 0}},
			{ID: "invalid", Version: 1, Space: "test-space", Values: []float32{0, 1}},
		}); err != nil {
			t.Fatal(err)
		}

		candidates, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}})
		if err == nil || candidates != nil {
			t.Fatalf("non-finite similarity %v produced candidates: %+v, %v", score, candidates, err)
		}
	}
}

func TestStorePreservesVersionsAndRemovalMarkers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "note", Version: 3, Space: "test-space", Values: []float32{1, 0}},
		{ID: "note", Version: 2, Space: "test-space", Values: []float32{0, 1}},
		{ID: "note", Version: 3, Space: "test-space", Values: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Remove(ctx, "note", 2); err != nil {
		t.Fatal(err)
	}

	record, err := store.Get(ctx, "note")
	if err != nil || record.Version != 3 || !slices.Equal(record.Values, []float32{1, 0}) {
		t.Fatalf("older or repeated change replaced current vector: %+v, %v", record, err)
	}

	if err := store.Remove(ctx, "note", 3); err != nil {
		t.Fatal(err)
	}

	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 3, Space: "test-space", Values: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(ctx, "note"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("late upsert resurrected removal: %v", err)
	}

	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 4, Space: "test-space", Values: []float32{0, 1}}}); err != nil {
		t.Fatal(err)
	}

	record, err = store.Get(ctx, "note")
	if err != nil || record.Version != 4 {
		t.Fatalf("newer vector was rejected: %+v, %v", record, err)
	}
}

func TestStorePreservesSpaceIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{
		{ID: "old", Version: 1, Space: "old-space", Values: []float32{1, 0}},
		{ID: "current", Version: 1, Space: "new-space", Values: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := store.Search(ctx, vector.Query{Space: "new-space", Values: []float32{1, 0}, Limit: 1})
	if err != nil || len(candidates) != 1 || candidates[0].ID != "current" {
		t.Fatalf("different embedding spaces were compared: %+v, %v", candidates, err)
	}

	if err := store.Upsert(ctx, []vector.Record{{ID: "old", Version: 1, Space: "new-space", Values: []float32{-1, 0}}}); err != nil {
		t.Fatal(err)
	}

	record, err := store.Get(ctx, "old")
	if err != nil || record.Space != "new-space" || record.Version != 1 || !slices.Equal(record.Values, []float32{-1, 0}) {
		t.Fatalf("space replacement at the same entry version = %+v, %v", record, err)
	}

	candidates, err = store.Search(ctx, vector.Query{Space: "old-space", Values: []float32{1, 0}})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("old space remained searchable: %+v, %v", candidates, err)
	}

	if err := store.Upsert(ctx, []vector.Record{{ID: "missing-space", Version: 1, Values: []float32{1, 0}}}); err == nil {
		t.Fatal("record without space accepted")
	}

	if _, err := store.Search(ctx, vector.Query{Values: []float32{1, 0}}); err == nil {
		t.Fatal("query without space accepted")
	}
}

func TestStoreOwnsRecordData(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := inmemory.New()

	values := []float32{1, 0}

	metadata := map[string]json.RawMessage{
		"model":    json.RawMessage(`"embedding-model"`),
		"provider": json.RawMessage(`{"name":"provider"}`),
	}
	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 1, Space: "test-space", Values: values, Metadata: metadata}}); err != nil {
		t.Fatal(err)
	}

	values[0] = 0
	metadata["model"][1] = 'x'
	metadata["extra"] = json.RawMessage(`true`)
	delete(metadata, "provider")

	for range 2 {
		record, err := store.Get(ctx, "note")
		if err != nil || !slices.Equal(record.Values, []float32{1, 0}) {
			t.Fatalf("stored vector was mutated: %+v, %v", record, err)
		}

		if len(record.Metadata) != 2 || string(record.Metadata["model"]) != `"embedding-model"` || string(record.Metadata["provider"]) != `{"name":"provider"}` {
			t.Fatalf("stored metadata was mutated: %+v", record.Metadata)
		}

		record.Values[0] = 0
		record.Metadata["model"][1] = 'x'
		record.Metadata["extra"] = json.RawMessage(`true`)
		delete(record.Metadata, "provider")
	}

	page, err := store.List(ctx, memory.Filter{})
	if err != nil {
		t.Fatal(err)
	}

	page.Records[0].Values[0] = 0
	page.Records[0].Metadata["model"][1] = 'x'
	page.Records[0].Metadata["extra"] = json.RawMessage(`true`)
	delete(page.Records[0].Metadata, "provider")

	record, err := store.Get(ctx, "note")
	if err != nil || len(record.Metadata) != 2 || string(record.Metadata["model"]) != `"embedding-model"` || string(record.Metadata["provider"]) != `{"name":"provider"}` {
		t.Fatalf("list output mutation changed stored metadata: %+v, %v", record, err)
	}

	candidates, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}})
	if err != nil || len(candidates) != 1 || candidates[0].Score != 1 {
		t.Fatalf("list output mutation changed stored vector: %+v, %v", candidates, err)
	}
}

func TestStoreRejectsInvalidVectorsWithoutPartialWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	store := inmemory.New()
	if err := store.Upsert(ctx, []vector.Record{{ID: "existing", Version: 1, Space: "test-space", Values: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	for _, values := range [][]float32{
		nil, {}, {0, 0}, {float32(math.NaN()), 1}, {float32(math.Inf(1)), 1},
	} {
		if err := store.Upsert(ctx, []vector.Record{
			{ID: "existing", Version: 2, Space: "test-space", Values: []float32{0, 1}},
			{ID: "invalid", Version: 1, Space: "test-space", Values: values},
		}); err == nil {
			t.Fatalf("invalid vector accepted: %v", values)
		}

		if _, err := store.Search(ctx, vector.Query{Space: "test-space", Values: values}); err == nil {
			t.Fatalf("invalid query vector accepted: %v", values)
		}
	}

	record, err := store.Get(ctx, "existing")
	if err != nil || record.Version != 1 || !slices.Equal(record.Values, []float32{1, 0}) {
		t.Fatalf("failed batch changed an existing vector: %+v, %v", record, err)
	}

	if _, err := store.Get(ctx, "invalid"); !errors.Is(err, vector.ErrNotFound) {
		t.Fatalf("failed batch inserted a vector: %v", err)
	}
}

func TestStoreHonorsCancellation(t *testing.T) {
	t.Parallel()

	store := inmemory.New()
	if err := store.Upsert(t.Context(), []vector.Record{{ID: "note", Version: 1, Space: "test-space", Values: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.Upsert(ctx, []vector.Record{{ID: "note", Version: 2, Space: "test-space", Values: []float32{0, 1}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upsert = %v", err)
	}

	if err := store.Remove(ctx, "note", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled removal = %v", err)
	}

	if _, err := store.Get(ctx, "note"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get = %v", err)
	}

	if _, err := store.List(ctx, memory.Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list = %v", err)
	}

	if _, err := store.Search(ctx, vector.Query{Space: "test-space", Values: []float32{1, 0}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search = %v", err)
	}

	record, err := store.Get(t.Context(), "note")
	if err != nil || record.Version != 1 {
		t.Fatalf("canceled writes changed the vector: %+v, %v", record, err)
	}
}
