package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

const (
	AgentName              = "agent"
	AgentEventType         = "agent.event"
	AgentFinishedEntryType = "agent.finished"
)

type Agent struct {
	factory     *agent.Factory
	checkpoints *checkpoint.Generator
}

type AgentOption func(*Agent)

func WithCheckpoints(generator *checkpoint.Generator) AgentOption {
	return func(a *Agent) { a.checkpoints = generator }
}

func NewAgent(factory *agent.Factory, options ...AgentOption) *Agent {
	a := &Agent{factory: factory}
	for _, option := range options {
		option(a)
	}

	return a
}

type AgentFinished struct {
	Usage     kit.Usage `json:"usage"`
	LastUsage kit.Usage `json:"last_usage"`
}

func (a *Agent) Execute(ctx context.Context, params ExecuteParams) error {
	var input kit.Message
	if err := json.Unmarshal(params.Input, &input); err != nil {
		return fmt.Errorf("decode agent input: %w", err)
	}

	if err := a.completeToolResults(ctx, params); err != nil {
		return err
	}

	agentMemory := memory.New(params.SessionID, params.ExecutionID, params.Entries, params.Events)

	built, err := a.factory.Build(ctx, agent.BuildRequest{
		SessionID:   params.SessionID,
		ExecutionID: params.ExecutionID,
		Memory:      agentMemory,
		Checkpoint: func(ctx context.Context) (*checkpoint.Checkpoint, error) {
			return a.checkpoint(ctx, params, agentMemory)
		},
	})
	if err != nil {
		return fmt.Errorf("build agent: %w", err)
	}

	stream := built.Stream(ctx, input)

	var runErr error
	for value := range stream.Iter() {
		if err := params.Events.Emit(ctx, params.ExecutionID, AgentEventType, value); err != nil {
			runErr = err

			break
		}
	}

	response, err := stream.Result()
	if runErr == nil {
		runErr = err
	}

	if runErr != nil {
		runErr = errors.Join(runErr, a.completeToolResults(context.WithoutCancel(ctx), params))
	}

	if err := params.Events.Commit(context.WithoutCancel(ctx), params.ExecutionID, AgentFinishedEntryType,
		AgentFinished{
			Usage: response.Usage,
			LastUsage: response.LastUsage,
		},
	); err != nil {
		return errors.Join(runErr, fmt.Errorf("record agent result: %w", err))
	}

	return runErr
}

var errUnrecordedToolResult = errors.New("tool result was not recorded before execution ended; the tool may have changed external state, so verify it before retrying")

func (*Agent) completeToolResults(ctx context.Context, params ExecuteParams) error {
	agentMemory := memory.New(params.SessionID, params.ExecutionID, params.Entries, params.Events)

	_, messages, err := agentMemory.Current(ctx)
	if err != nil {
		return fmt.Errorf("read context: %w", err)
	}

	calls := unresolvedToolCalls(messages)
	if len(calls) == 0 {
		return nil
	}

	latest, _, err := params.Entries.Latest(ctx, params.SessionID, memory.EntryType)
	if err != nil {
		return fmt.Errorf("find tool execution: %w", err)
	}

	agentMemory = memory.New(params.SessionID, latest.ExecutionID, params.Entries, params.Events)
	for _, call := range calls {
		result := kit.NewToolResult(call, kit.ToolOutput{}, errUnrecordedToolResult)
		message := kit.NewToolMessage(kit.NewToolResultContent(result))

		if err := agentMemory.Record(ctx, message); err != nil {
			return fmt.Errorf("record result for tool call %s: %w", call.ID, err)
		}
	}

	return nil
}

func unresolvedToolCalls(messages []kit.Message) []kit.ToolCall {
	results := make(map[string]struct{})
	index := len(messages) - 1

	for index >= 0 && messages[index].Role == kit.RoleTool {
		for _, result := range messages[index].ToolResults() {
			results[result.Call.ID] = struct{}{}
		}

		index--
	}

	if index < 0 || messages[index].Role != kit.RoleModel {
		return nil
	}

	calls := messages[index].ToolCalls()

	unresolved := make([]kit.ToolCall, 0, len(calls))
	for _, call := range calls {
		if _, ok := results[call.ID]; !ok {
			unresolved = append(unresolved, call)
		}
	}

	return unresolved
}
