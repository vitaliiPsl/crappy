package agent

import (
	"context"
	"fmt"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	adktool "github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type BuildRequest struct {
	SessionID   string
	ExecutionID string
	Memory      kit.Memory
	State       state.State
	Entries     entry.Store
	Events      event.Publisher
}

type ModelResolver interface {
	Resolve(context.Context) (kit.Model, error)
}

type Factory struct {
	config         kit.AgentConfig
	models         ModelResolver
	tools          []kit.Tool
	toolMiddleware []ToolMiddleware
	options        []adkagent.Option
	contributors   []Contributor
}

func New(config kit.AgentConfig, models ModelResolver, options ...Option) *Factory {
	factory := &Factory{
		config: config,
		models: models,
	}

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

	options := append(agentOptions(f.config), f.options...)
	tools := append([]kit.Tool(nil), f.tools...)
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

		tools = append(tools, contribution.Tools...)
		options = append(options, contribution.Options...)
	}

	tools, err = wrapTools(build, tools, f.toolMiddleware)
	if err != nil {
		return nil, err
	}

	return adkagent.New(
		build.Model,
		request.Memory,
		adktool.NewSet(tools...),
		options...,
	)
}

func agentOptions(config kit.AgentConfig) []adkagent.Option {
	options := make([]adkagent.Option, 0, 5)

	if config.Instructions != "" {
		options = append(options, adkagent.WithInstructions(config.Instructions))
	}

	if config.Temperature != nil {
		options = append(options, adkagent.WithTemperature(*config.Temperature))
	}

	if config.MaxOutputTokens != nil {
		options = append(options, adkagent.WithMaxOutputTokens(*config.MaxOutputTokens))
	}

	if config.Thinking != nil {
		options = append(options, adkagent.WithThinking(*config.Thinking))
	}

	if config.OutputSchema != nil {
		options = append(options, adkagent.WithOutputSchema(*config.OutputSchema))
	}

	return options
}
