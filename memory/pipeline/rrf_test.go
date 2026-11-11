package pipeline_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/pipeline"
)

func TestRRFCombinesRankings(t *testing.T) {
	t.Parallel()

	fusion, err := pipeline.NewRRF()
	if err != nil {
		t.Fatal(err)
	}

	a := memory.Entry{ID: "a", Scope: "personal", Content: "canonical a", Source: "fixture", Version: 3}
	b := memory.Entry{ID: "b", Scope: "personal", Content: "canonical b", Version: 2}
	c := memory.Entry{ID: "c", Scope: "personal", Content: "canonical c", Version: 1}
	d := memory.Entry{ID: "d", Scope: "personal", Content: "canonical d", Version: 1}
	lists := [][]memory.SearchResult{
		{{Entry: a, Score: 1000}, {Entry: a, Score: 900}, {Entry: b, Score: 0.01}, {Entry: d}},
		{{Entry: b, Score: -100}, {Entry: c, Score: 0.9}, {Entry: a, Score: -300}},
	}

	results, err := fusion.Merge(t.Context(), lists)
	if err != nil || len(results) != 4 {
		t.Fatalf("fused results = %+v, %v", results, err)
	}

	want := []memory.SearchResult{
		{Entry: b, Score: 1.0/62 + 1.0/61},
		{Entry: a, Score: 1.0/61 + 1.0/63},
		{Entry: c, Score: 1.0 / 62},
		{Entry: d, Score: 1.0 / 63},
	}
	for position, expected := range want {
		got := results[position]
		if math.Abs(got.Score-expected.Score) > 1e-12 {
			t.Fatalf("fused position %d = %+v, want %+v", position, got, expected)
		}

		assertEntry(t, got.Entry, expected.Entry)
	}

	if lists[0][0].Score != 1000 || lists[1][2].Score != -300 {
		t.Fatalf("fusion mutated input lists: %+v", lists)
	}

	assertEntry(t, lists[0][0].Entry, a)
	assertEntry(t, lists[1][2].Entry, a)
}

func TestRRFBreaksTiesByID(t *testing.T) {
	t.Parallel()

	fusion, err := pipeline.NewRRF(pipeline.WithRankConstant(0))
	if err != nil {
		t.Fatal(err)
	}

	results, err := fusion.Merge(t.Context(), [][]memory.SearchResult{
		{{Entry: memory.Entry{ID: "b", Version: 1}}},
		{{Entry: memory.Entry{ID: "a", Version: 1}}},
	})
	if err != nil || len(results) != 2 || results[0].Entry.ID != "a" || results[1].Entry.ID != "b" || results[0].Score != 1 || results[1].Score != 1 {
		t.Fatalf("tied results = %+v, %v", results, err)
	}
}

func TestRRFValidatesConfigurationAndCancellation(t *testing.T) {
	t.Parallel()

	if fusion, err := pipeline.NewRRF(pipeline.WithRankConstant(-1)); fusion != nil || err == nil {
		t.Fatalf("negative rank constant accepted: %v, %v", fusion, err)
	}

	fusion, err := pipeline.NewRRF()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if results, err := fusion.Merge(ctx, nil); results != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fusion = %+v, %v", results, err)
	}
}
