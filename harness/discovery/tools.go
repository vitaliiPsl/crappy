package discovery

import (
	"errors"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

const (
	SearchToolName = "tool_search"
	LoadToolName   = "tool_load"
)

type searchInput struct {
	Query string `json:"query" jsonschema:"Search words describing the operation, service, or tool name"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum matching tools to load, from 1 to 10; defaults to 5"`
}

type loadInput struct {
	Names []string `json:"names" jsonschema:"Exact registered tool names to load, up to 10 at once"`
}

func NewTools(set *ToolSet) []kit.Tool {
	return []kit.Tool{
		tool.MustNew(SearchToolName,
			"Search discoverable tool names, descriptions, and parameters, then load matching tools for the next model call. Returns summaries; complete callable schemas are exposed automatically. Search again if necessary.",
			func(_ *kit.RunContext, input searchInput) ([]Match, error) {
				return set.Search(input.Query, input.Limit)
			}),
		tool.MustNew(LoadToolName,
			"Load tools by their exact registered names for the next model call, without searching. Use this when a tool name is already known from documentation or prior work. Loading does not execute the tools.",
			func(_ *kit.RunContext, input loadInput) ([]Match, error) {
				if len(input.Names) == 0 {
					return nil, errors.New("names must contain at least one tool name")
				}

				return set.Load(input.Names...)
			}),
	}
}
