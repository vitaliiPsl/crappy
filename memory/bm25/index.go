package bm25

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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
	defaultSaturation          = 1.2
	defaultLengthNormalization = 0.75
)

type Option func(*Index)

// WithSaturation sets k1; zero makes repeated occurrences contribute no extra weight.
func WithSaturation(k1 float64) Option {
	return func(index *Index) {
		index.k1 = k1
	}
}

// WithLengthNormalization sets b in [0, 1]; zero disables length normalization.
func WithLengthNormalization(b float64) Option {
	return func(index *Index) {
		index.b = b
	}
}

type Index struct {
	k1 float64
	b  float64

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

func New(ctx context.Context, store memory.Store, options ...Option) (*Index, error) {
	index := &Index{
		k1:        defaultSaturation,
		b:         defaultLengthNormalization,
		documents: make(map[string]document),
		postings:  make(map[string]map[string]struct{}),
		removed:   make(map[string]uint64),
	}
	for _, option := range options {
		option(index)
	}

	if math.IsNaN(index.k1) || math.IsInf(index.k1, 0) || index.k1 < 0 {
		return nil, fmt.Errorf("BM25 saturation must be finite and non-negative: %g", index.k1)
	}

	if math.IsNaN(index.b) || index.b < 0 || index.b > 1 {
		return nil, fmt.Errorf("BM25 length normalization must be in [0, 1]: %g", index.b)
	}

	filter := memory.Filter{Limit: memory.DefaultPageSize}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		page, err := store.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("load BM25 entries: %w", err)
		}

		for _, entry := range page.Entries {
			if err := index.Upsert(ctx, entry); err != nil {
				return nil, fmt.Errorf("index entry %q: %w", entry.ID, err)
			}
		}

		if page.NextID == "" {
			return index, nil
		}

		filter.AfterID = page.NextID
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

	frequencyWeight := 1 / (i.k1 + 1)
	normalizationWeight := i.k1 / (i.k1 + 1)

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

			normalization := 1 - i.b + i.b*float64(doc.length)/averageLength
			tf := float64(doc.terms[term])
			scores[id] += idf * (tf / (tf*frequencyWeight + normalizationWeight*normalization))
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
