package agent

import (
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/toolset"
)

type ToolSet struct {
	registry   kit.ToolSet
	build      BuildContext
	middleware []ToolMiddleware
}

var _ kit.ToolSet = (*ToolSet)(nil)

func NewToolSet(build BuildContext, middleware ...ToolMiddleware) *ToolSet {
	return &ToolSet{registry: toolset.New(), build: build, middleware: middleware}
}

func (s *ToolSet) Register(tools ...kit.Tool) error {
	prepared, err := wrapTools(s.build, tools, s.middleware)
	if err != nil {
		return err
	}

	return s.registry.Register(prepared...)
}

func (s *ToolSet) Tools() []kit.Tool {
	return s.registry.Tools()
}

func (s *ToolSet) Lookup(name string) (kit.Tool, bool) {
	return s.registry.Lookup(name)
}
