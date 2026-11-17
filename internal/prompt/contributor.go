package prompt

import (
	"context"

	"github.com/vitaliipsl/crappy/harness/agent"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const instructions = `Work autonomously: inspect available context, retrieve missing facts, and make reasonable assumptions for decisions that are easy to revise.
Use ask_user only when information cannot be obtained from available sources, different answers materially change the work, and guessing would likely produce an unusable result or an expensive mistake.
Do not start each request with questions or ask for minor preferences. When a question is necessary, explain the decision briefly and offer a few useful choices when appropriate. The user can also answer in their own words.
ask_user waits for the user's answer. Continue the work using that answer; cancellation is not an answer.`

type Contributor struct{}

func NewContributor() *Contributor {
	return &Contributor{}
}

func (*Contributor) Contribute(_ context.Context, build agent.BuildContext) (agent.Contribution, error) {
	return agent.Contribution{
		Source:       "User interaction",
		Instructions: []string{instructions},
		Tools:        []kit.Tool{NewTool(build.Request.Interactions, build.Request.ExecutionID)},
	}, nil
}
