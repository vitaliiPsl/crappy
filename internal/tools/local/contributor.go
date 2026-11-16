package local

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/background"
	"github.com/vitaliipsl/crappy/internal/tools/bash"
	"github.com/vitaliipsl/crappy/internal/tools/filesystem"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

var _ agent.Contributor = (*Contributor)(nil)

const instructionsTemplate = `- Working directory: %q
- Platform: %s
- Shell: %q
- Timezone: %s`

type Contributor struct {
	jobs *background.Manager
}

func NewContributor(jobs *background.Manager) *Contributor {
	return &Contributor{jobs: jobs}
}

func (c *Contributor) Contribute(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
	directory, err := workdir.Resolve(ctx, build.Request.State)
	if err != nil {
		return agent.Contribution{}, err
	}

	shell := cmp.Or(os.Getenv("SHELL"), "sh")
	timezone := time.Now().Format("MST (UTC-07:00)")
	instructions := fmt.Sprintf(instructionsTemplate, directory, runtime.GOOS, shell, timezone)

	shellTool, err := background.NewTool(c.jobs, build.Request.SessionID, bash.New(directory, shell))
	if err != nil {
		return agent.Contribution{}, err
	}

	return agent.Contribution{
		Source:       "Environment",
		Instructions: []string{instructions},
		Tools: append(
			[]kit.Tool{shellTool},
			filesystem.New(directory)...,
		),
	}, nil
}
