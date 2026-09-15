package automatic

import (
	"context"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
)

type ContributorOption func(*Contributor)

func WithTrigger(trigger Trigger) ContributorOption {
	return func(contributor *Contributor) {
		contributor.trigger = trigger
	}
}

func WithEstimator(estimator Estimator) ContributorOption {
	return func(contributor *Contributor) {
		contributor.estimator = estimator
	}
}

type Contributor struct {
	trigger   Trigger
	estimator Estimator
}

var _ agent.Contributor = (*Contributor)(nil)

func New(options ...ContributorOption) *Contributor {
	contributor := &Contributor{
		trigger:   WhenInputExceedsRatio(DefaultThresholdRatio),
		estimator: &CharacterEstimator{},
	}

	for _, option := range options {
		option(contributor)
	}

	return contributor
}

func (c *Contributor) Contribute(
	_ context.Context,
	build agent.BuildContext,
) (agent.Contribution, error) {
	inputLimit := build.Model.Config().InputLimit

	return agent.Contribution{
		Options: []adkagent.Option{
			adkagent.WithOnModelRequest(
				c.beforeModelRequest(build.Request.Checkpoint, inputLimit),
			),
		},
	}, nil
}

func (c *Contributor) beforeModelRequest(
	checkpoint func(context.Context) error,
	inputLimit int,
) kit.OnModelRequest {
	return func(
		run *kit.RunContext,
		request kit.ModelRequest,
	) (kit.ModelRequest, error) {
		if !c.trigger.ShouldTrigger(TriggerRequest{
			InputTokens: c.inputTokens(run, request),
			InputLimit:  inputLimit,
		}) {
			return request, nil
		}

		if err := checkpoint(run.Context); err != nil {
			return kit.ModelRequest{}, err
		}

		messages, err := run.Memory.Context(run.Context)
		if err != nil {
			return kit.ModelRequest{}, err
		}

		request.Messages = messages

		return request, nil
	}
}

func (c *Contributor) inputTokens(
	run *kit.RunContext,
	request kit.ModelRequest,
) int64 {
	if run.LastUsage.InputTokens == 0 {
		return int64(c.estimator.EstimateRequest(request))
	}

	tokens := run.LastUsage.InputTokens + run.LastUsage.OutputTokens
	for _, message := range messagesAfterLastModelResponse(run.Messages) {
		tokens += int64(c.estimator.EstimateMessage(message))
	}

	return tokens
}

func messagesAfterLastModelResponse(messages []kit.Message) []kit.Message {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == kit.RoleModel {
			return messages[index+1:]
		}
	}

	return nil
}
