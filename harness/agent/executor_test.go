package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func TestAgentCompletesMissingToolResults(t *testing.T) {
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
			Type:        execution.StartedEntryType,
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

	if err := (NewExecutor(nil)).completeToolResults(context.Background(), execution.ExecuteParams{SessionID: "session-1", ExecutionID: "execution-1", Entries: store, Events: event.NewBus("session-1", store)}); err != nil {
		t.Fatalf("completeToolResults() error = %v", err)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if got, want := len(entries), 4; got != want {
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

func TestAgentRepairsToolHistoryBeforeBuilding(t *testing.T) {
	t.Parallel()

	store := &messageFailingStore{Store: entry.NewMemoryStore(), failure: errors.New("storage unavailable"), failing: true}
	bus := event.NewBus("session-1", store)
	call := kit.NewToolCall("call-1", "write", nil)

	payload, err := json.Marshal(kit.NewModelMessage(kit.NewToolCallContent(call)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Store.Append(context.Background(), "session-1", entry.Entry{
		ExecutionID: "previous", Type: memory.EntryType, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	var builds int

	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
	}})
	configured := NewExecutor(New(kit.AgentConfig{}, staticModelResolver{model}, WithContributors(
		ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
			builds++

			return Contribution{}, nil
		}),
	)))

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "next", Input: agentInput(t, "continue"), Entries: store, Events: bus}
	if _, err := configured.Execute(context.Background(), params); !errors.Is(err, store.failure) {
		t.Fatalf("failed history repair = %v", err)
	}

	if builds != 0 {
		t.Fatal("agent was built before its history was usable")
	}

	store.failing = false

	if _, err := configured.Execute(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	if builds != 1 {
		t.Fatalf("agent builds = %d", builds)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	var repaired kit.Message
	if err := json.Unmarshal(entries[1].Payload, &repaired); err != nil {
		t.Fatal(err)
	}

	if entries[1].ExecutionID != "previous" || len(repaired.ToolResults()) != 1 || repaired.ToolResults()[0].Call.ID != call.ID {
		t.Fatalf("repaired history entry = %+v", entries[1])
	}
}

type messageFailingStore struct {
	entry.Store
	failure error
	failing bool
}

func (s *messageFailingStore) Append(ctx context.Context, sessionID string, values ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range values {
		if s.failing && value.Type == memory.EntryType {
			return nil, s.failure
		}
	}

	return s.Store.Append(ctx, sessionID, values...)
}

func agentInput(t *testing.T, text string) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(text)))
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}
