package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestExecutorCompletesMissingToolResults(t *testing.T) {
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
			Type:        execution.EntryType,
			Payload:     json.RawMessage(`{"id":"execution-1","executor":"agent","input":null,"status":"running"}`),
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

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "execution-1", Entries: store, Events: event.NewBus("session-1", store)}
	history := NewHistory(params.SessionID, params.ExecutionID, params.Entries, params.Events)

	if err := (NewExecutor(nil)).completeToolResults(context.Background(), history, params); err != nil {
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

func TestExecutorRepairsToolHistoryBeforeBuilding(t *testing.T) {
	t.Parallel()

	store := &failingStore{Store: entry.NewMemoryStore(), failingType: MessageEntryType, failure: errors.New("storage unavailable")}
	bus := event.NewBus("session-1", store)
	call := kit.NewToolCall("call-1", "write", nil)

	payload, err := json.Marshal(kit.NewModelMessage(kit.NewToolCallContent(call)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Store.Append(context.Background(), "session-1", entry.Entry{
		ExecutionID: "previous", Type: MessageEntryType, Payload: payload,
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
	if err := configured.Execute(context.Background(), params); !errors.Is(err, store.failure) {
		t.Fatalf("failed history repair = %v", err)
	}

	if builds != 0 {
		t.Fatal("agent was built before its history was usable")
	}

	store.failingType = ""

	if err := configured.Execute(context.Background(), params); err != nil {
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

	for _, value := range entries[2:] {
		if value.ExecutionID != "next" {
			t.Fatalf("repair changed attribution of subsequent entries: %+v", value)
		}
	}
}

func TestExecutorContinuesConversationAcrossExecutions(t *testing.T) {
	t.Parallel()

	usages := []kit.Usage{{InputTokens: 10, OutputTokens: 2}, {InputTokens: 20, OutputTokens: 4}}
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("first response")), FinishReason: kit.FinishReasonStop, Usage: usages[0],
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("second response")), FinishReason: kit.FinishReasonStop, Usage: usages[1],
		}},
	)
	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	executor := NewExecutor(New(kit.AgentConfig{}, staticModelResolver{model}))

	executionIDs := []string{"execution-1", "execution-2"}
	for i, input := range []string{"first input", "second input"} {
		params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: executionIDs[i], Input: agentInput(t, input), Entries: store, Events: bus}
		if err := executor.Execute(context.Background(), params); err != nil {
			t.Fatal(err)
		}
	}

	var sent []string
	for _, message := range model.CallAt(1).Messages {
		sent = append(sent, message.TextContent().Text)
	}

	if !slices.Equal(sent, []string{"first input", "first response", "second input"}) {
		t.Fatalf("second request messages = %q", sent)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("entries = %+v, error = %v, want input and response per execution", entries, err)
	}

	for i, value := range entries {
		if value.Type != MessageEntryType || value.ExecutionID != executionIDs[i/2] {
			t.Fatalf("entry %d = %+v, want message from %s", i, value, executionIDs[i/2])
		}
	}

	for i, value := range []entry.Entry{entries[1], entries[3]} {
		var response kit.Message
		if err := json.Unmarshal(value.Payload, &response); err != nil || response.Usage != usages[i] {
			t.Fatalf("%s response = %s, error = %v, want usage %+v", executionIDs[i], value.Payload, err, usages[i])
		}
	}
}

func TestExecutorPublishesAgentEvents(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Events: []kit.ModelEvent{kit.NewModelContentDeltaEvent(kit.NewTextContent("hello"))},
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Close)

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "execution-1", Input: agentInput(t, "hi"), Entries: store, Events: bus}
	if err := NewExecutor(New(kit.AgentConfig{}, staticModelResolver{model})).Execute(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	for i, wantType := range []string{MessageEntryType, EventType, MessageEntryType, EventType} {
		received := receiveEvent(t, subscription)
		if received.Type != wantType || received.ExecutionID != "execution-1" {
			t.Fatalf("event %d = %+v, want %s from execution-1", i, received, wantType)
		}

		if i != 1 {
			continue
		}

		var delta kit.AgentEvent
		if err := json.Unmarshal(received.Payload, &delta); err != nil {
			t.Fatal(err)
		}

		if delta.Content == nil || delta.Content.Text == nil || delta.Content.Text.Text != "hello" {
			t.Fatalf("content delta = %+v, want hello", delta)
		}
	}
}

type failingStore struct {
	entry.Store
	failingType string
	failure     error
}

func (s *failingStore) Append(ctx context.Context, sessionID string, values ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range values {
		if value.Type == s.failingType {
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
