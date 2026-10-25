package automatic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
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
	generator *compaction.Generator
	trigger   Trigger
	estimator compaction.Estimator
}

var _ agent.Contributor = (*Contributor)(nil)

func New(generator *compaction.Generator, options ...ContributorOption) *Contributor {
	contributor := &Contributor{
		generator: generator,
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

		err := c.compact(run, build)
		return err == nil, err
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

		return c.compact(run, build)
	}
}

func (c *Contributor) compact(
	run *kit.RunContext,
	build agent.BuildRequest,
) error {
	previous, messages, err := loadCompactionContext(run.Context, build.SessionID, build.Entries)
	if err != nil {
		return fmt.Errorf("read compaction context: %w", err)
	}

	if err := build.Events.Emit(run.Context, build.ExecutionID, compaction.StartedEventType, nil); err != nil {
		return err
	}

	created, err := c.generator.Generate(run.Context, previous, messages)
	if created != nil {
		run.Usage.Add(created.Usage)
	}
	if err != nil {
		return err
	}

	if err := build.Events.Commit(run.Context, build.ExecutionID, compaction.EntryType, created); err != nil {
		return fmt.Errorf("commit compaction: %w", err)
	}

	return nil
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

func loadCompactionContext(ctx context.Context, sessionID string, entries entry.Store) (*compaction.Compaction, []kit.Message, error) {
	latest, found, err := entries.Latest(ctx, sessionID, compaction.EntryType)
	if err != nil {
		return nil, nil, err
	}

	var previous *compaction.Compaction
	var messages []kit.Message
	var afterID uint64
	if found {
		previous = &compaction.Compaction{}
		if err := json.Unmarshal(latest.Payload, previous); err != nil {
			return nil, nil, fmt.Errorf("decode compaction entry %d: %w", latest.ID, err)
		}
		messages = append(messages, previous.Retained...)
		afterID = latest.ID
	}

	stored, err := entries.List(ctx, sessionID, afterID)
	if err != nil {
		return nil, nil, err
	}
	for _, value := range stored {
		if value.Type != agent.MessageEntryType {
			continue
		}
		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			return nil, nil, fmt.Errorf("decode message entry %d: %w", value.ID, err)
		}
		messages = append(messages, message)
	}

	return previous, messages, nil
}
