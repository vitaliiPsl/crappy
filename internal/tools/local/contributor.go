package local

import (
	"context"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/tools/bash"
	"github.com/vitaliipsl/crappy/internal/tools/filesystem"
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

	return agent.Contribution{
		Instructions: []string{fmt.Sprintf("Working directory: %q", directory)},
		Tools: append(
			[]kit.Tool{bash.New(directory)},
			filesystem.New(directory)...,
		),
	}, nil
}
