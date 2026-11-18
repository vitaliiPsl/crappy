package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/agent"
)

type Contributor struct {
	manager *Manager
}

func NewContributor(manager *Manager) *Contributor {
	return &Contributor{manager: manager}
}

func (c *Contributor) Contribute(_ context.Context, _ agent.BuildContext) (agent.Contribution, error) {
	contribution := agent.Contribution{Source: "MCP"}

	for _, server := range c.manager.Servers() {
		if errors.Is(server.Error, ErrAuthorizationRequired) {
			contribution.Instructions = append(contribution.Instructions,
				fmt.Sprintf("MCP server %q needs authorization. Its tools are unavailable until the user runs `crappy mcp login %s`.", server.Name, server.Name))

			continue
		}

		if server.Error != nil {
			contribution.Instructions = append(contribution.Instructions,
				fmt.Sprintf("MCP server %q is unavailable (%v). Its tools are unavailable; do not claim to have used them.", server.Name, server.Error))

			continue
		}

		contribution.Tools = append(contribution.Tools, server.Tools...)
		if server.Instructions != "" {
			contribution.Instructions = append(contribution.Instructions,
				fmt.Sprintf("MCP server %q instructions:\n%s", server.Name, server.Instructions))
		}
	}

	return contribution, nil
}
