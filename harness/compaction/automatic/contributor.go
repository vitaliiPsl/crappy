package automatic

import (
	"context"
	"errors"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/token"
)

type ContributorOption func(*Contributor)

func WithTrigger(trigger Trigger) ContributorOption {
	return func(contributor *Contributor) {
		contributor.trigger = trigger
	}
}

func WithEstimator(estimator token.Estimator) ContributorOption {
	return func(contributor *Contributor) {
		contributor.estimator = estimator
	}
}

type Contributor struct {
	compactor *compaction.Compactor
	trigger   Trigger
	estimator token.Estimator
}

var _ agent.Contributor = (*Contributor)(nil)

func New(compactor *compaction.Compactor, options ...ContributorOption) *Contributor {
	contributor := &Contributor{
		compactor: compactor,
		trigger:   WhenInputExceedsRatio(DefaultThresholdRatio),
		estimator: token.NewCharacterEstimator(),
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
			adkagent.WithOnModelRequestReady(
				c.beforeModelRequest(build.Request.History, inputLimit),
			),
			adkagent.WithOnModelError(
				c.onModelError(build.Request.History),
			),
		},
	}, nil
}

func (c *Contributor) beforeModelRequest(
	history *agent.History,
	inputLimit int,
) kit.OnModelRequestReady {
	return func(
		run *kit.RunContext,
		request kit.ModelRequest,
	) (bool, error) {
		if !c.trigger.ShouldTrigger(TriggerRequest{
			InputTokens: c.inputTokens(request),
			InputLimit:  inputLimit,
		}) {
			return false, nil
		}

		return history.Compact(run.Context, c.compactor)
	}
}

func (c *Contributor) onModelError(
	history *agent.History,
) kit.OnModelError {
	return func(
		run *kit.RunContext,
		_ kit.ModelRequest,
		err error,
	) error {
		if !errors.Is(err, kit.ErrContextLength) {
			return err
		}

		compacted, compactErr := history.Compact(run.Context, c.compactor)
		if compactErr != nil {
			return compactErr
		}

		if !compacted {
			return err
		}

		return nil
	}
}

func (c *Contributor) inputTokens(request kit.ModelRequest) int64 {
	measured := lastMeasuredResponse(request.Messages)
	if measured < 0 {
		return int64(token.EstimateRequest(c.estimator, request))
	}

	usage := request.Messages[measured].Usage
	tokens := usage.InputTokens + usage.OutputTokens

	for _, message := range request.Messages[measured+1:] {
		tokens += int64(token.EstimateMessage(c.estimator, message))
	}

	return tokens
}

func lastMeasuredResponse(messages []kit.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == kit.RoleModel && messages[index].Usage.InputTokens != 0 {
			return index
		}
	}

	return -1
}
