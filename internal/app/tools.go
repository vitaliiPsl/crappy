package app

import (
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/prompt"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func toolTimeout(config tools.Config) agent.ToolMiddleware {
	timeout := tools.Timeout(config)

	return func(build agent.BuildContext, underlying kit.Tool) (kit.Tool, error) {
		if underlying.Definition().Name == prompt.ToolName {
			return underlying, nil
		}

		return timeout(build, underlying)
	}
}
