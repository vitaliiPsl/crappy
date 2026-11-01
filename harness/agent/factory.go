package agent

import (
	"context"
	"fmt"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

type BuildRequest struct {
	SessionID   string
	ExecutionID string
	State       state.State
	History     *History
}

type ModelResolver interface {
	Resolve(context.Context) (kit.Model, error)
}

type Factory struct {
	config         kit.AgentConfig
	models         ModelResolver
	toolMiddleware []ToolMiddleware
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
	model, err := f.models.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	build := BuildContext{
		Request: request,
		Model:   model,
	}
	options := agentOptions(f.config)

	var tools []kit.Tool

	for i, contributor := range f.contributors {
		contribution, err := contributor.Contribute(ctx, build)
		if err != nil {
			return nil, fmt.Errorf("contributor %d: %w", i, err)
		}

		tools = append(tools, contribution.Tools...)
		options = append(options, adkagent.WithInstructionsSource(contribution.Source, contribution.Instructions...))
		options = append(options, contribution.Options...)
	}

	tools, err = wrapTools(build, tools, f.toolMiddleware)
	if err != nil {
		return nil, err
	}

	built, err := adkagent.New(model, request.History, append(options, adkagent.WithTools(tools...))...)
	if err != nil {
		return nil, err
	}

	return built, nil
}

func agentOptions(config kit.AgentConfig) []adkagent.Option {
	options := []adkagent.Option{adkagent.WithInstructions(config.Instructions)}

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
