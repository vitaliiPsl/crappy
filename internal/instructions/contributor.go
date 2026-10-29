package instructions

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

var _ agent.Contributor = (*Contributor)(nil)

type Contributor struct{}

func NewContributor() *Contributor {
	return &Contributor{}
}

func (*Contributor) Contribute(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
	directory, err := workdir.Resolve(ctx, build.Request.State)
	if err != nil {
		return agent.Contribution{}, err
	}

	instructions := instructionFiles(directory)
	if instructions == "" {
		return agent.Contribution{}, nil
	}

	return agent.Contribution{
		Instructions: []string{instructions},
	}, nil
}
