package execution

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestReconcileRecordsMissingToolResults(t *testing.T) {
	t.Parallel()

	firstCall := kit.NewToolCall("call-1", "first", nil)
	secondCall := kit.NewToolCall("call-2", "second", nil)
	modelMessage := kit.NewModelMessage(
		kit.NewToolCallContent(firstCall),
		kit.NewToolCallContent(secondCall),
	)
	firstResult := kit.NewToolMessage(kit.NewToolResultContent(
		kit.NewToolResult(firstCall, kit.NewToolOutput(kit.NewTextContent("done")), nil),
	))

	modelPayload, err := json.Marshal(modelMessage)
	if err != nil {
		t.Fatalf("encode model message: %v", err)
	}

	resultPayload, err := json.Marshal(firstResult)
	if err != nil {
		t.Fatalf("encode tool result: %v", err)
	}

	store := entry.NewMemoryStore()

	_, err = store.Append(
		context.Background(),
		"session-1",
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        StartedEntryType,
			Payload:     json.RawMessage(`{}`),
		},
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        "message",
			Payload:     modelPayload,
		},
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        "message",
			Payload:     resultPayload,
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if err := newExecutor(store, nil, nil).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if got, want := len(entries), 5; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}

	if entries[3].Type != "message" || entries[3].ExecutionID != "execution-1" {
		t.Fatalf("reconciled entry = %#v", entries[3])
	}

	var message kit.Message
	if err := json.Unmarshal(entries[3].Payload, &message); err != nil {
		t.Fatalf("decode reconciled message: %v", err)
	}

	results := message.ToolResults()
	if len(results) != 1 || results[0].Call.ID != secondCall.ID {
		t.Fatalf("reconciled results = %+v, want second tool result", results)
	}

	if results[0].Error != errUnrecordedToolResult.Error() {
		t.Fatalf("tool result error = %q, want %q", results[0].Error, errUnrecordedToolResult)
	}

	if entries[4].Type != FinishedEntryType {
		t.Fatalf("last entry = %#v, want execution finished", entries[4])
	}
}

func TestUnresolvedToolCalls(t *testing.T) {
	t.Parallel()

	firstCall := kit.NewToolCall("call-1", "first", nil)
	secondCall := kit.NewToolCall("call-2", "second", nil)
	toolCalls := kit.NewModelMessage(
		kit.NewToolCallContent(firstCall),
		kit.NewToolCallContent(secondCall),
	)
	firstResult := kit.NewToolMessage(kit.NewToolResultContent(
		kit.NewToolResult(firstCall, kit.ToolOutput{}, nil),
	))
	secondResult := kit.NewToolMessage(kit.NewToolResultContent(
		kit.NewToolResult(secondCall, kit.ToolOutput{}, nil),
	))

	tests := []struct {
		name     string
		messages []kit.Message
		want     []string
	}{
		{
			name:     "no results",
			messages: []kit.Message{toolCalls},
			want:     []string{firstCall.ID, secondCall.ID},
		},
		{
			name:     "partial results",
			messages: []kit.Message{toolCalls, firstResult},
			want:     []string{secondCall.ID},
		},
		{
			name:     "all results",
			messages: []kit.Message{toolCalls, firstResult, secondResult},
		},
		{
			name:     "final model response",
			messages: []kit.Message{toolCalls, kit.NewModelMessage(kit.NewTextContent("done"))},
		},
		{
			name:     "non-tool boundary",
			messages: []kit.Message{toolCalls, firstResult, kit.NewUserMessage(kit.NewTextContent("next"))},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := unresolvedToolCalls(test.messages)
			if len(calls) != len(test.want) {
				t.Fatalf("unresolved calls = %+v, want IDs %v", calls, test.want)
			}

			for index, want := range test.want {
				if calls[index].ID != want {
					t.Fatalf("unresolved call %d ID = %q, want %q", index, calls[index].ID, want)
				}
			}
		})
	}
}
