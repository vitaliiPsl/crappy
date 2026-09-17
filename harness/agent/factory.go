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

type ModelResolver interface {
	Resolve(context.Context) (kit.Model, error)
}

type Factory struct {
	models       ModelResolver
	tools        []kit.Tool
	options      []adkagent.Option
	contributors []Contributor
}

func New(models ModelResolver, options ...Option) *Factory {
	factory := &Factory{models: models}

	for _, option := range options {
		option(factory)
	}

	return factory
}

func (f *Factory) Build(ctx context.Context, request BuildRequest) (kit.Agent, error) {
	resolved, err := f.models.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	options := append([]adkagent.Option(nil), f.options...)
	build := BuildContext{
		Request: request,
		Model:   resolved,
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
