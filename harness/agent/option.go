package agent

import (
	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Option func(*Factory)

func WithInstructions(text ...string) Option {
	return WithAgentOptions(adkagent.WithInstructions(text...))
}

func WithInstructionsSource(source string, text ...string) Option {
	return WithAgentOptions(adkagent.WithInstructionsSource(source, text...))
}

func WithTools(tools ...kit.Tool) Option {
	return func(factory *Factory) {
		factory.tools = append(factory.tools, tools...)
	}
}

func WithToolMiddleware(middleware ...ToolMiddleware) Option {
	return func(factory *Factory) {
		factory.toolMiddleware = append(factory.toolMiddleware, middleware...)
	}
}

func WithAgentOptions(options ...adkagent.Option) Option {
	return func(factory *Factory) {
		factory.options = append(factory.options, options...)
	}
}

func WithContributors(contributors ...Contributor) Option {
	return func(factory *Factory) {
		factory.contributors = append(factory.contributors, contributors...)
	}
}
