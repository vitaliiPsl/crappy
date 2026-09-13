package agent

import (
	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Factory struct {
	model   kit.Model
	tools   kit.ToolSet
	options []adkagent.Option
}

func New(model kit.Model, tools kit.ToolSet, options ...adkagent.Option) *Factory {
	return &Factory{
		model:   model,
		tools:   tools,
		options: append([]adkagent.Option(nil), options...),
	}
}

func (f *Factory) Build(memory kit.Memory) (kit.Agent, error) {
	return adkagent.New(f.model, memory, f.tools, f.options...)
}
