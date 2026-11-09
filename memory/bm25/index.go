package bm25

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/vitaliipsl/crappy/memory"
)

var (
	_ memory.Indexer   = (*Index)(nil)
	_ memory.Retriever = (*Index)(nil)
)

const (
	k1 = 1.2
	b  = 0.75
)

type Index struct {
	mu          sync.RWMutex
	documents   map[string]document
	postings    map[string]map[string]struct{}
	removed     map[string]uint64
	totalLength int
}

type document struct {
	scope   string
	version uint64
	terms   map[string]int
	length  int
}

func New() *Index {
	return &Index{
		documents: make(map[string]document),
		postings:  make(map[string]map[string]struct{}),
		removed:   make(map[string]uint64),
	}
}

func (i *Index) Upsert(ctx context.Context, entry memory.Entry) error {
	if entry.ID == "" || entry.Version == 0 {
		return errors.New("indexed entry ID and version are required")
	}

	terms := tokenize(entry.Content)

	doc := document{
		scope:   entry.Scope,
		version: entry.Version,
		terms:   make(map[string]int),
		length:  len(terms),
	}

	for _, term := range terms {
		doc.terms[term]++
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	previous := i.documents[entry.ID]
	if entry.Version <= i.removed[entry.ID] || entry.Version <= previous.version {
		return nil
	}

	i.removeDocument(entry.ID)
	i.documents[entry.ID] = doc
	i.totalLength += doc.length

	for term := range doc.terms {
		if i.postings[term] == nil {
			i.postings[term] = make(map[string]struct{})
		}

		i.postings[term][entry.ID] = struct{}{}
	}

	return nil
}

func (i *Index) Remove(ctx context.Context, id string, throughVersion uint64) error {
	if id == "" || throughVersion == 0 {
		return errors.New("removed entry ID and version are required")
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	i.removed[id] = max(i.removed[id], throughVersion)
	if i.documents[id].version <= throughVersion {
		i.removeDocument(id)
	}

	return nil
}

// Retrieve returns higher BM25 scores first, with entry IDs breaking ties.
func (i *Index) Retrieve(ctx context.Context, query memory.Query) ([]memory.Candidate, error) {
	if query.Limit < 0 {
		return nil, errors.New("retrieval limit cannot be negative")
	}

	limit := query.Limit
	if limit == 0 {
		limit = memory.DefaultSearchLimit
	}

	terms := tokenize(query.Text)
	slices.Sort(terms)
	terms = slices.Compact(terms)

	i.mu.RLock()
	defer i.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(i.documents) == 0 {
		return []memory.Candidate{}, nil
	}

	documentCount := float64(len(i.documents))
	averageLength := float64(i.totalLength) / documentCount

	scores := make(map[string]float64)
	for _, term := range terms {
		postings := i.postings[term]

		documentFrequency := float64(len(postings))

		idf := math.Log1p((documentCount - documentFrequency + 0.5) / (documentFrequency + 0.5))
		for id := range postings {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			doc := i.documents[id]
			if !query.MatchesFilters(id, doc.scope) {
				continue
			}

			normalization := k1 * (1 - b + b*float64(doc.length)/averageLength)
			tf := float64(doc.terms[term])
			scores[id] += idf * tf * (k1 + 1) / (tf + normalization)
		}
	}

	candidates := make([]memory.Candidate, 0, len(scores))
	for id, score := range scores {
		candidates = append(candidates, memory.Candidate{ID: id, Version: i.documents[id].version, Score: score})
	}

	slices.SortFunc(candidates, func(a, b memory.Candidate) int {
		if order := cmp.Compare(b.Score, a.Score); order != 0 {
			return order
		}

		return strings.Compare(a.ID, b.ID)
	})

	return candidates[:min(limit, len(candidates))], nil
}

func (i *Index) removeDocument(id string) {
	doc := i.documents[id]
	for term := range doc.terms {
		delete(i.postings[term], id)

		if len(i.postings[term]) == 0 {
			delete(i.postings, term)
		}
	}

	i.totalLength -= doc.length
	delete(i.documents, id)
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
