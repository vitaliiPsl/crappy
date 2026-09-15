package agent

import (
	"context"
	"fmt"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	adktool "github.com/vitaliiPsl/crappy-adk/x/tool"
)

type BuildRequest struct {
	SessionID   string
	ExecutionID string
	Memory      kit.Memory
	Checkpoint  func(context.Context) error
}

type Factory struct {
	model        kit.Model
	tools        []kit.Tool
	options      []adkagent.Option
	contributors []Contributor
}

func New(model kit.Model, options ...Option) *Factory {
	factory := &Factory{model: model}

	for _, option := range options {
		option(factory)
	}

	return factory
}

func (f *Factory) Build(ctx context.Context, request BuildRequest) (kit.Agent, error) {
	options := append([]adkagent.Option(nil), f.options...)
	build := BuildContext{
		Request: request,
		Model:   f.model,
	}

	for i, contributor := range f.contributors {
		contribution, err := contributor.Contribute(ctx, build)
		if err != nil {
			return nil, fmt.Errorf("contributor %d: %w", i, err)
		}

		if len(contribution.Instructions) > 0 {
			options = append(options, adkagent.WithInstructions(contribution.Instructions...))
		}

		if len(contribution.Tools) > 0 {
			options = append(options, adkagent.WithTools(contribution.Tools...))
		}

		options = append(options, contribution.Options...)
	}

	tools := adktool.NewSet(f.tools...)

	return adkagent.New(build.Model, build.Request.Memory, tools, options...)
}
