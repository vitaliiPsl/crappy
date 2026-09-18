package agent

import (
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type ToolMiddleware func(BuildContext, kit.Tool) (kit.Tool, error)

func wrapTools(
	build BuildContext,
	tools []kit.Tool,
	middleware []ToolMiddleware,
) ([]kit.Tool, error) {
	wrapped := make([]kit.Tool, len(tools))
	for index, current := range tools {
		wrapped[index] = current
		for middlewareIndex := len(middleware) - 1; middlewareIndex >= 0; middlewareIndex-- {
			var err error

			wrapped[index], err = middleware[middlewareIndex](build, wrapped[index])
			if err != nil {
				return nil, fmt.Errorf("wrap tool %q: %w", current.Definition().Name, err)
			}
		}
	}

	return wrapped, nil
}
