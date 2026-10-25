package automatic

import (
	"context"
	"errors"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
)

type ContributorOption func(*Contributor)

func WithTrigger(trigger Trigger) ContributorOption {
	return func(contributor *Contributor) {
		contributor.trigger = trigger
	}
}

func WithEstimator(estimator compaction.Estimator) ContributorOption {
	return func(contributor *Contributor) {
		contributor.estimator = estimator
	}
}

type Contributor struct {
	executor  *compactionexec.Executor
	trigger   Trigger
	estimator compaction.Estimator
}

var _ agent.Contributor = (*Contributor)(nil)

func New(executor *compactionexec.Executor, options ...ContributorOption) *Contributor {
	contributor := &Contributor{
		executor:  executor,
		trigger:   WhenInputExceedsRatio(DefaultThresholdRatio),
		estimator: &compaction.CharacterEstimator{},
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
				c.beforeModelRequest(build.Request, inputLimit),
			),
			adkagent.WithOnModelError(
				c.onModelError(build.Request),
			),
		},
	}, nil
}

func (c *Contributor) beforeModelRequest(
	build agent.BuildRequest,
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

		return c.compact(run, build)
	}
}

func (c *Contributor) onModelError(
	build agent.BuildRequest,
) kit.OnModelError {
	return func(
		run *kit.RunContext,
		_ kit.ModelRequest,
		err error,
	) error {
		if !errors.Is(err, kit.ErrContextLength) {
			return err
		}

		compacted, compactErr := c.compact(run, build)
		if compactErr != nil {
			return compactErr
		}

		if !compacted {
			return err
		}

		return nil
	}
}

func (c *Contributor) compact(run *kit.RunContext, build agent.BuildRequest) (bool, error) {
	output, err := c.executor.Execute(run.Context, execution.ExecuteParams{
		SessionID:   build.SessionID,
		ExecutionID: build.ExecutionID,
		Entries:     build.Entries,
		Events:      build.Events,
	})
	result := output.(compactionexec.Result)
	run.Usage.Add(result.Usage)

	return result.Compacted, err
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
