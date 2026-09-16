package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/memory"
)

var errUnrecordedToolResult = errors.New("tool result was not recorded before execution ended; the tool may have changed external state, so verify it before retrying")

func (s *Session) reconcileExecutions(ctx context.Context) error {
	entries, err := s.entries.List(ctx, s.state.ID, 0)
	if err != nil {
		return fmt.Errorf("list entries: %w", err)
	}

	executionID := unfinishedExecution(entries)
	if executionID == "" {
		return nil
	}

	if err := s.reconcileTools(ctx, executionID); err != nil {
		return fmt.Errorf("reconcile tools for execution %s: %w", executionID, err)
	}

	if err := s.Commit(
		ctx,
		executionID,
		ExecutionFinishedEntryType,
		ExecutionFinished{Outcome: ExecutionInterrupted},
	); err != nil {
		return fmt.Errorf("interrupt execution %s: %w", executionID, err)
	}

	return nil
}

func (s *Session) reconcileTools(ctx context.Context, executionID string) error {
	agentMemory := memory.New(s.state.ID, executionID, s.entries, s.events)

	messages, err := agentMemory.History(ctx)
	if err != nil {
		return fmt.Errorf("read history: %w", err)
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

func unfinishedExecution(entries []entry.Entry) string {
	for index := len(entries) - 1; index >= 0; index-- {
		switch value := entries[index]; value.Type {
		case ExecutionStartedEntryType:
			return value.ExecutionID
		case ExecutionFinishedEntryType:
			return ""
		}
	}

	return ""
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
