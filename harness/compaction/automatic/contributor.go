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
			InputTokens: c.inputTokens(run, request),
			InputLimit:  inputLimit,
		}) {
			return false, nil
		}

		return c.compact(run, history)
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

		compacted, compactErr := c.compact(run, history)
		if compactErr != nil {
			return compactErr
		}

		if !compacted {
			return err
		}

		return nil
	}
}

func (c *Contributor) compact(run *kit.RunContext, history *agent.History) (bool, error) {
	generated, compacted, err := history.Compact(run.Context, c.compactor)
	run.Usage.Add(generated.Usage)

	return compacted, err
}

func (c *Contributor) inputTokens(
	run *kit.RunContext,
	request kit.ModelRequest,
) int64 {
	if run.LastUsage.InputTokens == 0 {
		return int64(token.EstimateRequest(c.estimator, request))
	}

	tokens := run.LastUsage.InputTokens + run.LastUsage.OutputTokens
	for _, message := range messagesAfterLastModelResponse(run.Messages) {
		tokens += int64(token.EstimateMessage(c.estimator, message))
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
