package discovery

import (
	"sync"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type ToolSet struct {
	registry  kit.ToolSet
	deferTool func(kit.Tool) bool
	entries   []entry
	byName    map[string]entry
	selected  map[string]bool
	mu        sync.RWMutex
}

var _ kit.ToolSet = (*ToolSet)(nil)

// NewToolSet registers search/load tools through registry, including its middleware.
func NewToolSet(registry kit.ToolSet, deferTool func(kit.Tool) bool) (*ToolSet, error) {
	set := &ToolSet{
		registry:  registry,
		deferTool: deferTool,
		byName:    make(map[string]entry),
		selected:  make(map[string]bool),
	}
	set.indexRegisteredTools(registry.Tools())

	if err := registry.Register(NewTools(set)...); err != nil {
		return nil, err
	}

	return set, nil
}

func (s *ToolSet) Register(tools ...kit.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.registry.Register(tools...); err != nil {
		return err
	}

	s.indexRegisteredTools(tools)

	return nil
}

func (s *ToolSet) indexRegisteredTools(tools []kit.Tool) {
	for _, tool := range tools {
		prepared, _ := s.registry.Lookup(tool.Definition().Name)
		if !s.deferTool(prepared) {
			continue
		}

		item := newEntry(prepared.Definition())
		s.entries = append(s.entries, item)
		s.byName[item.match.Name] = item
	}
}

func (s *ToolSet) Tools() []kit.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var exposed []kit.Tool
	for _, tool := range s.registry.Tools() {
		name := tool.Definition().Name
		if _, deferred := s.byName[name]; !deferred || s.selected[name] {
			exposed = append(exposed, tool)
		}
	}

	return exposed
}

func (s *ToolSet) Lookup(name string) (kit.Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.registry.Lookup(name)
}
