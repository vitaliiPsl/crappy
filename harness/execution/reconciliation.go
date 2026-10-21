package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/memory"
)

var errUnrecordedToolResult = errors.New("tool result was not recorded before execution ended; the tool may have changed external state, so verify it before retrying")

func (e *Executor) Reconcile(ctx context.Context) error {
	executionID, err := e.unfinishedExecution(ctx)
	if err != nil {
		return err
	}

	if executionID == "" {
		return nil
	}

	if err := e.reconcileTools(ctx, executionID); err != nil {
		return fmt.Errorf("reconcile tools for execution %s: %w", executionID, err)
	}

	if err := e.events.Commit(
		ctx,
		executionID,
		FinishedEntryType,
		Finished{Outcome: Interrupted},
	); err != nil {
		return fmt.Errorf("interrupt execution %s: %w", executionID, err)
	}

	return nil
}

func (e *Executor) reconcileTools(ctx context.Context, executionID string) error {
	agentMemory := memory.New(e.sessionID, executionID, e.entries, e.events)

	_, messages, err := agentMemory.Current(ctx)
	if err != nil {
		return fmt.Errorf("read context: %w", err)
	}

	for _, call := range unresolvedToolCalls(messages) {
		result := kit.NewToolResult(call, kit.ToolOutput{}, errUnrecordedToolResult)
		message := kit.NewToolMessage(kit.NewToolResultContent(result))

		if err := agentMemory.Record(ctx, message); err != nil {
			return fmt.Errorf("record result for tool call %s: %w", call.ID, err)
		}
	}

	return nil
}

func (e *Executor) unfinishedExecution(ctx context.Context) (string, error) {
	started, found, err := e.entries.Latest(ctx, e.sessionID, StartedEntryType)
	if err != nil || !found {
		return "", err
	}

	finished, _, err := e.entries.Latest(ctx, e.sessionID, FinishedEntryType)
	if err != nil {
		return "", err
	}

	if finished.ID > started.ID {
		return "", nil
	}

	return started.ExecutionID, nil
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
