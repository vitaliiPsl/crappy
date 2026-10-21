package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/memory"
)

type Executor struct {
	sessionID   string
	entries     entry.Store
	events      event.Publisher
	agents      *agent.Factory
	checkpoints *checkpoint.Generator
}

func NewExecutor(
	sessionID string,
	entries entry.Store,
	events event.Publisher,
	agents *agent.Factory,
	checkpoints *checkpoint.Generator,
) *Executor {
	return &Executor{
		sessionID:   sessionID,
		entries:     entries,
		events:      events,
		agents:      agents,
		checkpoints: checkpoints,
	}
}

func (e *Executor) Start(ctx context.Context, input kit.Message) (*Execution, error) {
	executionID := uuid.NewString()
	agentMemory := memory.New(e.sessionID, executionID, e.entries, e.events)

	agent, err := e.agents.Build(ctx, agent.BuildRequest{
		SessionID:   e.sessionID,
		ExecutionID: executionID,
		Memory:      agentMemory,
		Checkpoint: func(ctx context.Context) (*checkpoint.Checkpoint, error) {
			return e.checkpoint(ctx, executionID, agentMemory)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build agent: %w", err)
	}

	executionContext, cancel := context.WithCancel(ctx)
	execution := &Execution{
		id:     executionID,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	if err := e.events.Commit(
		ctx,
		executionID,
		StartedEntryType,
		Started{Input: input},
	); err != nil {
		cancel()

		return nil, fmt.Errorf("start execution: %w", err)
	}

	go e.run(executionContext, execution, agent, input)

	return execution, nil
}

func (e *Executor) run(
	ctx context.Context,
	execution *Execution,
	agent kit.Agent,
	input kit.Message,
) {
	defer close(execution.done)
	defer execution.cancel()

	stream := agent.Stream(ctx, input)

	var runErr error
	for agentEvent := range stream.Iter() {
		if err := e.events.Emit(ctx, execution.id, AgentEventType, agentEvent); err != nil {
			runErr = err

			break
		}
	}

	response, streamErr := stream.Result()
	if runErr == nil {
		runErr = streamErr
	}

	if runErr != nil {
		if err := e.reconcileTools(context.WithoutCancel(ctx), execution.id); err != nil {
			runErr = fmt.Errorf("reconcile tools: %w", err)
		}
	}

	finished := Finished{
		Outcome:   Completed,
		Usage:     response.Usage,
		LastUsage: response.LastUsage,
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			finished.Outcome = Cancelled
		} else {
			finished.Outcome = Failed
			finished.Error = runErr.Error()
		}
	}

	_ = e.events.Commit(
		context.WithoutCancel(ctx),
		execution.id,
		FinishedEntryType,
		finished,
	)
}
