package pipeline

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/vitaliipsl/crappy/memory"
)

var _ memory.Merger = (*RRF)(nil)

type RRF struct {
	rankConstant int
}

func NewRRF(rankConstant int) (*RRF, error) {
	if rankConstant < 0 {
		return nil, errors.New("rank constant cannot be negative")
	}

	return &RRF{
		rankConstant: rankConstant,
	}, nil
}

func (f *RRF) Merge(ctx context.Context, lists [][]memory.SearchResult) ([]memory.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	combined := make(map[string]memory.SearchResult)
	for _, list := range lists {
		seen := make(map[string]bool)

		rank := 0
		for _, result := range list {
			id := result.Entry.ID
			if seen[id] {
				continue
			}

			seen[id] = true
			rank++

			fused, exists := combined[id]
			if !exists {
				fused.Entry = result.Entry
			}

			fused.Score += 1 / (float64(f.rankConstant) + float64(rank))
			combined[id] = fused
		}
	}

	results := make([]memory.SearchResult, 0, len(combined))
	for _, result := range combined {
		results = append(results, result)
	}

	slices.SortFunc(results, func(a, b memory.SearchResult) int {
		if order := cmp.Compare(b.Score, a.Score); order != 0 {
			return order
		}

		return strings.Compare(a.Entry.ID, b.Entry.ID)
	})

	return results, nil
}
