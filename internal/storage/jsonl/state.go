package jsonl

import (
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type sessionState struct {
	mu    sync.Mutex
	index *transcriptIndex
}

type transcriptIndex struct {
	lastID uint64
	latest map[string]position
}

type position struct {
	id     uint64
	offset int64
}

func newTranscriptIndex() *transcriptIndex {
	return &transcriptIndex{latest: make(map[string]position)}
}

func (i *transcriptIndex) add(value entry.Entry, offset int64) bool {
	i.lastID = value.ID
	i.latest[value.Type] = position{id: value.ID, offset: offset}

	return true
}

func (i *transcriptIndex) offsetAfter(afterID uint64) int64 {
	var best position

	for _, current := range i.latest {
		if current.id <= afterID && current.id > best.id {
			best = current
		}
	}

	return best.offset
}
