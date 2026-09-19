package jsonl

import (
	"sync"

	"github.com/vitaliipsl/crappy/harness/entry"
)

type sessionState struct {
	mu      sync.Mutex
	entries []entry.Entry
}
