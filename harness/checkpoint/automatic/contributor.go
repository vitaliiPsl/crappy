package automatic

import (
	"context"
	"errors"
	"fmt"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

type ContributorOption func(*Contributor)

func WithTrigger(trigger Trigger) ContributorOption {
	return func(contributor *Contributor) {
		contributor.trigger = trigger
	}
}

func WithEstimator(estimator checkpoint.Estimator) ContributorOption {
	return func(contributor *Contributor) {
		contributor.estimator = estimator
	}
}

type Contributor struct {
	generator *checkpoint.Generator
	trigger   Trigger
	estimator checkpoint.Estimator
}

var _ agent.Contributor = (*Contributor)(nil)

func New(generator *checkpoint.Generator, options ...ContributorOption) *Contributor {
	contributor := &Contributor{
		generator: generator,
		trigger:   WhenInputExceedsRatio(DefaultThresholdRatio),
		estimator: &checkpoint.CharacterEstimator{},
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

		return c.compact(run, build, request)
	}
}

func (c *Contributor) onModelError(
	build agent.BuildRequest,
) kit.OnModelError {
	return func(
		run *kit.RunContext,
		request kit.ModelRequest,
		err error,
	) (kit.ModelRequest, error) {
		if !errors.Is(err, kit.ErrContextLength) {
			return kit.ModelRequest{}, err
		}

		return c.compact(run, build, request)
	}
}

func (c *Contributor) compact(
	run *kit.RunContext,
	build agent.BuildRequest,
	request kit.ModelRequest,
) (kit.ModelRequest, error) {
	agentMemory := memory.New(build.SessionID, build.ExecutionID, build.Entries, build.Events)

	previous, messages, err := agentMemory.Current(run.Context)
	if err != nil {
		return kit.ModelRequest{}, fmt.Errorf("read checkpoint context: %w", err)
	}

	if err := build.Events.Emit(run.Context, build.ExecutionID, checkpoint.StartedEventType, nil); err != nil {
		return kit.ModelRequest{}, err
	}

	created, err := c.generator.Generate(run.Context, previous, messages)
	if err != nil {
		return kit.ModelRequest{}, err
	}

	if err := build.Events.Commit(run.Context, build.ExecutionID, checkpoint.EntryType, created); err != nil {
		return kit.ModelRequest{}, fmt.Errorf("commit checkpoint: %w", err)
	}

	run.Usage.Add(created.Usage)

	messages, err = run.Memory.Context(run.Context)
	if err != nil {
		return kit.ModelRequest{}, err
	}

	request.Messages = messages

	return request, nil
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
