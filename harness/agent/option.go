package agent

type Option func(*Factory)

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
