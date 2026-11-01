package agent

import (
	"fmt"
	"slices"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

// ToolMiddleware also runs when an agent is built for inspection, so side
// effects belong in the wrapped tool's invocation.
type ToolMiddleware func(BuildContext, kit.Tool) (kit.Tool, error)

func wrapTools(
	build BuildContext,
	tools []kit.Tool,
	middleware []ToolMiddleware,
) ([]kit.Tool, error) {
	wrapped := make([]kit.Tool, len(tools))
	for i, tool := range tools {
		for _, wrap := range slices.Backward(middleware) {
			var err error
			if tool, err = wrap(build, tool); err != nil {
				return nil, fmt.Errorf("wrap tool %q: %w", tools[i].Definition().Name, err)
			}
		}

		wrapped[i] = tool
	}

	return wrapped, nil
}
