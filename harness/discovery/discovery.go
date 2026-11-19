package discovery

import (
	"errors"
	"fmt"
	"slices"
)

const (
	DefaultLimit = 5
	MaxLimit     = 10
)

type Match struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Search loads its matches; a zero limit uses DefaultLimit.
func (s *ToolSet) Search(query string, limit int) ([]Match, error) {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, errors.New("search query must contain words")
	}

	if limit == 0 {
		limit = DefaultLimit
	}

	if limit < 1 || limit > MaxLimit {
		return nil, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}

	s.mu.RLock()
	names := rank(s.entries, terms, limit)
	s.mu.RUnlock()

	return s.Load(names...)
}

// Load leaves selection unchanged if any name is unknown.
func (s *ToolSet) Load(names ...string) ([]Match, error) {
	if len(names) > MaxLimit {
		return nil, fmt.Errorf("load at most %d tools at once", MaxLimit)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	matches := make([]Match, 0, len(names))
	for _, name := range names {
		item, found := s.byName[name]
		if !found {
			return nil, fmt.Errorf("tool %q is not in the discovery catalog; use tool_search to find its registered name", name)
		}

		if !slices.ContainsFunc(matches, func(match Match) bool { return match.Name == name }) {
			matches = append(matches, item.match)
		}
	}

	for _, match := range matches {
		s.selected[match.Name] = true
	}

	return matches, nil
}
