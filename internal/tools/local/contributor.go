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
	"github.com/vitaliipsl/crappy/internal/tools/bash"
	"github.com/vitaliipsl/crappy/internal/tools/filesystem"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

var _ agent.Contributor = (*Contributor)(nil)

const instructionsTemplate = `# Environment

- Working directory: %q
- Platform: %s
- Shell: %q
- Timezone: %s`

type Contributor struct{}

func NewContributor() *Contributor {
	return &Contributor{}
}

func (*Contributor) Contribute(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
	directory, err := workdir.Resolve(ctx, build.Request.State)
	if err != nil {
		return agent.Contribution{}, err
	}

	shell := cmp.Or(os.Getenv("SHELL"), "sh")
	timezone := time.Now().Format("MST (UTC-07:00)")
	instructions := fmt.Sprintf(instructionsTemplate, directory, runtime.GOOS, shell, timezone)

	return agent.Contribution{
		Instructions: []string{instructions},
		Tools: append(
			[]kit.Tool{bash.New(directory, shell)},
			filesystem.New(directory)...,
		),
	}, nil
}
