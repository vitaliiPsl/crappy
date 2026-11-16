package background

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/agent"
)

const instructions = `Use background_timeout_seconds on supported tools to start work in the background with its own timeout. Omit it for foreground execution.
Background jobs outlive the execution that launches them, but stop when Crappy stops. Jobs are kept in memory.
Completion notices arrive in this conversation. Wait for a notice instead of polling; use job_get to read the result, job_list to inspect jobs, and job_cancel to stop one.`

type Contributor struct {
	manager *Manager
}

func NewContributor(manager *Manager) *Contributor {
	return &Contributor{manager: manager}
}

func (c *Contributor) Contribute(_ context.Context, build agent.BuildContext) (agent.Contribution, error) {
	return agent.Contribution{
		Source:       "Background jobs",
		Instructions: []string{instructions},
		Tools:        NewTools(c.manager, build.Request.SessionID),
	}, nil
}
