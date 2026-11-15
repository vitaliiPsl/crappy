package schedule

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/agent"
)

const instructions = `Use scheduling tools for future work instead of sleeping or background processes. Check the clock through the shell when resolving relative dates.
Scheduled work runs in this conversation and may run late while the laptop is asleep or Crappy is stopped.
Only confirm a schedule after schedule_create succeeds.`

type Contributor struct {
	manager *Manager
}

func NewContributor(manager *Manager) *Contributor {
	return &Contributor{manager: manager}
}

func (c *Contributor) Contribute(_ context.Context, build agent.BuildContext) (agent.Contribution, error) {
	return agent.Contribution{
		Source:       "Scheduling",
		Instructions: []string{instructions},
		Tools:        NewTools(c.manager, build.Request.SessionID),
	}, nil
}
