package app

import (
	"strings"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/discovery"
	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/prompt"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func discoverMCPTools(registry kit.ToolSet) (kit.ToolSet, error) {
	return discovery.NewToolSet(registry, func(tool kit.Tool) bool {
		return strings.HasPrefix(tool.Definition().Name, mcp.ToolPrefix)
	})
}

func toolTimeout(config tools.Config) agent.ToolMiddleware {
	timeout := tools.Timeout(config)

	return func(build agent.BuildContext, underlying kit.Tool) (kit.Tool, error) {
		if underlying.Definition().Name == prompt.ToolName {
			return underlying, nil
		}

		return timeout(build, underlying)
	}
}
