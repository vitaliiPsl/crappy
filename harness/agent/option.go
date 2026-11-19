package agent

import "github.com/vitaliiPsl/crappy-adk/kit"

type Option func(*Factory)

func WithToolSetFactory(build func(kit.ToolSet) (kit.ToolSet, error)) Option {
	return func(factory *Factory) {
		factory.toolSetFactory = build
	}
}

func WithToolMiddleware(middleware ...ToolMiddleware) Option {
	return func(factory *Factory) {
		factory.toolMiddleware = append(factory.toolMiddleware, middleware...)
	}
}

func WithContributors(contributors ...Contributor) Option {
	return func(factory *Factory) {
		factory.contributors = append(factory.contributors, contributors...)
	}
}
