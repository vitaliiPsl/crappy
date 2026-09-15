package agent

import (
	"context"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
)

type BuildContext struct {
	Request BuildRequest
	Model   kit.Model
}

type Contribution struct {
	Instructions []string
	Tools        []kit.Tool
	Options      []adkagent.Option
}

type Contributor interface {
	Contribute(context.Context, BuildContext) (Contribution, error)
}

type ContributorFunc func(context.Context, BuildContext) (Contribution, error)

func (fn ContributorFunc) Contribute(
	ctx context.Context,
	build BuildContext,
) (Contribution, error) {
	return fn(ctx, build)
}
