package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
	adktool "github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestExecutorUsesSessionInteractions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	call := kit.NewToolCall("question", "ask", map[string]any{"text": "Which deadline?"})
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewToolCallContent(call)), FinishReason: kit.FinishReasonToolCall,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
		}},
	)
	contributor := ContributorFunc(func(_ context.Context, build BuildContext) (Contribution, error) {
		return Contribution{Tools: []kit.Tool{
			adktool.MustNew("ask", "Ask a question", func(run *kit.RunContext, question interaction.Question) (interaction.Answer, error) {
				return build.Request.Interactions.Ask(run.Context, build.Request.ExecutionID, question)
			}),
		}}, nil
	})
	opened, _ := newSteeringAgentSession(ctx, t, model, contributor)

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	current, err := opened.Execute(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent("prepare a report"))))
	if err != nil {
		t.Fatal(err)
	}

	var request interaction.Request
	for request.ID == "" {
		select {
		case value, open := <-subscription.Events():
			if !open {
				t.Fatalf("subscription closed while waiting for a request: %v", subscription.Err())
			}

			if value.Type == interaction.RequestedEntryType {
				if err := json.Unmarshal(value.Payload, &request); err != nil {
					t.Fatal(err)
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	if request.ExecutionID != current.ID() || request.SessionID != opened.ID() {
		t.Fatalf("request = %+v", request)
	}

	if err := opened.Respond(ctx, request.ID, interaction.Answer{Text: "Friday"}); err != nil {
		t.Fatal(err)
	}

	if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("execution = %s, %v", status, err)
	}

	if model.CallCount() != 2 {
		t.Fatalf("model calls = %d, want a follow-up after answering", model.CallCount())
	}

	for _, message := range model.CallAt(1).Messages {
		for _, result := range message.ToolResults() {
			if result.Call.ID == call.ID && result.Error == "" && reflect.DeepEqual(result.Output.Structured, map[string]any{"text": "Friday"}) {
				return
			}
		}
	}

	t.Fatal("the next model call did not receive the user's answer")
}

func TestExecutorConsumesSteeringBeforeNextModelCall(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	releaseTool := make(chan struct{})

	release := sync.OnceFunc(func() { close(releaseTool) })
	defer release()

	wait := adktool.MustNew("wait", "Wait for release", func(run *kit.RunContext, _ struct{}) (string, error) {
		close(started)

		select {
		case <-releaseTool:
			return "tool finished", nil
		case <-run.Done():
			return "", run.Err()
		}
	})
	call := kit.NewToolCall("call", "wait", nil)
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewToolCallContent(call)), FinishReason: kit.FinishReasonToolCall,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("steering handled")), FinishReason: kit.FinishReasonStop,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("queued work handled")), FinishReason: kit.FinishReasonStop,
		}},
	)
	opened, store := newSteeringAgentSession(ctx, t, model, contribute(Contribution{Tools: []kit.Tool{wait}}))

	first, err := opened.Execute(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent("initial"))))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	ordinary, err := opened.Execute(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent("later"))))
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"focus on scheduling", "also inspect cancellation"} {
		if err := opened.Steer(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent(text)))); err != nil {
			t.Fatal(err)
		}
	}

	release()

	if status, err := ordinary.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("queued execution = %s, %v", status, err)
	}

	if status, err := first.Status(); err != nil || status != execution.Completed {
		t.Fatalf("original execution = %s, %v", status, err)
	}

	model.AssertCallCount(t, 3)

	messages := model.CallAt(1).Messages
	if len(messages) != 5 || messages[2].Role != kit.RoleTool || messages[3].TextContent().Text != "focus on scheduling" || messages[4].TextContent().Text != "also inspect cancellation" {
		t.Fatalf("steering was not delivered after tool results: %+v", messages)
	}

	stored, err := store.List(ctx, "session", 0)
	if err != nil {
		t.Fatal(err)
	}

	deliveries := 0

	executionIDs := make(map[string]bool)
	for _, value := range stored {
		if value.Type == execution.EntryType {
			executionIDs[value.ExecutionID] = true
		}

		if value.Type != MessageEntryType {
			continue
		}

		var message kit.Message
		if err := json.Unmarshal(value.Payload, &message); err != nil {
			t.Fatal(err)
		}

		text := kit.ContentsText(message.Content)
		if text == "focus on scheduling" || text == "also inspect cancellation" {
			deliveries++

			if value.ExecutionID != first.ID() {
				t.Fatalf("steering changed execution identity: %+v", value)
			}
		}
	}

	if deliveries != 2 || len(executionIDs) != 2 {
		t.Fatalf("steering deliveries = %d, executions = %d", deliveries, len(executionIDs))
	}
}

func TestExecutorQueuesSteeringThatMissesLastBoundary(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	releaseModel := make(chan struct{})

	release := sync.OnceFunc(func() { close(releaseModel) })
	defer release()

	firstRequest := true
	gate := adkagent.WithOnModelRequestReady(func(run *kit.RunContext, _ kit.ModelRequest) (bool, error) {
		if firstRequest {
			firstRequest = false

			close(started)

			select {
			case <-releaseModel:
			case <-run.Done():
				return false, run.Err()
			}
		}

		return false, nil
	})
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("first finished")), FinishReason: kit.FinishReasonStop,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("follow-up finished")), FinishReason: kit.FinishReasonStop,
		}},
	)
	opened, _ := newSteeringAgentSession(ctx, t, model, contribute(Contribution{Options: []adkagent.Option{gate}}))

	first, err := opened.Execute(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent("initial"))))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := opened.Steer(ctx, execution.NewRequest(ExecutorName, kit.NewUserMessage(kit.NewTextContent("late steering")))); err != nil {
		t.Fatal(err)
	}

	release()

	if status, err := first.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("first execution = %s, %v", status, err)
	}

	records, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	nextID := ""
	for _, value := range records {
		if value.Type == execution.EntryType && value.ExecutionID != first.ID() {
			nextID = value.ExecutionID

			break
		}
	}

	next, err := opened.Execution(ctx, nextID)
	if err != nil {
		t.Fatal(err)
	}

	if status, err := next.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("fallback execution = %s, %v", status, err)
	}

	model.AssertCallCount(t, 2)

	if messages := model.CallAt(1).Messages; len(messages) != 3 || messages[1].TextContent().Text != "first finished" || messages[2].TextContent().Text != "late steering" {
		t.Fatalf("late steering did not use normal queue delivery: %+v", messages)
	}
}

func newSteeringAgentSession(ctx context.Context, t *testing.T, model kit.Model, contributors ...Contributor) (*session.Session, *entry.MemoryStore) {
	t.Helper()

	states := state.NewMemoryStore()
	if err := states.Save(ctx, state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	store := entry.NewMemoryStore()
	registry := execution.NewRegistry()
	registry.Register(ExecutorName, NewExecutor(New(kit.AgentConfig{}, staticModelResolver{model}, WithContributors(contributors...))))

	events := event.NewBroker("session", store)
	interactions := interaction.NewBroker(ctx, "session", events)

	opened, err := session.New(ctx, "session", state.New(states, "session"), store, events, interactions, registry)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(opened.Close)

	return opened, store
}

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

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "execution-1", Entries: store, Events: event.NewBroker("session-1", store)}
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
	broker := event.NewBroker("session-1", store)
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

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "next", Input: agentInput(t, "continue"), Entries: store, Events: broker}
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
	broker := event.NewBroker("session-1", store)
	executor := NewExecutor(New(kit.AgentConfig{}, staticModelResolver{model}))

	executionIDs := []string{"execution-1", "execution-2"}
	for i, input := range []string{"first input", "second input"} {
		params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: executionIDs[i], Input: agentInput(t, input), Entries: store, Events: broker}
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

func TestExecutorResumesWithoutDuplicatingInputOrCompletedTools(t *testing.T) {
	t.Parallel()

	call := kit.NewToolCall("call-1", "write", nil)
	tool := kittest.NewTool(t, "write", "Write a file", kittest.ToolResult{Result: "written"})
	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewToolCallContent(call)), FinishReason: kit.FinishReasonToolCall,
		}},
		kittest.ModelResult{Error: context.Canceled},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
		}},
	)
	store := entry.NewMemoryStore()
	broker := event.NewBroker("session", store)
	factory := New(kit.AgentConfig{}, staticModelResolver{model}, WithContributors(
		ContributorFunc(func(context.Context, BuildContext) (Contribution, error) {
			return Contribution{Tools: []kit.Tool{tool}}, nil
		}),
	))
	params := execution.ExecuteParams{
		SessionID: "session", ExecutionID: "work", Input: agentInput(t, "write the file"), Entries: store, Events: broker,
	}

	if err := NewExecutor(factory).Execute(context.Background(), params); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted execution = %v", err)
	}

	if err := NewExecutor(factory).Execute(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	tool.AssertCallCount(t, 1)
	model.AssertCallCount(t, 3)

	messages := model.CallAt(2).Messages
	if len(messages) != 3 || messages[0].TextContent().Text != "write the file" || len(messages[2].ToolResults()) != 1 {
		t.Fatalf("resumed model request = %+v", messages)
	}

	entries, err := store.List(context.Background(), "session", 0)
	if err != nil || len(entries) != 4 {
		t.Fatalf("resumed history = %+v, %v", entries, err)
	}

	for _, value := range entries {
		if value.ExecutionID != params.ExecutionID || value.Type != MessageEntryType {
			t.Fatalf("resumed history changed execution identity: %+v", value)
		}
	}
}

func TestExecutorResumesFinishedRunWithoutModelCall(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
	}})
	store := entry.NewMemoryStore()
	factory := New(kit.AgentConfig{}, staticModelResolver{model})
	params := execution.ExecuteParams{
		SessionID: "session", ExecutionID: "work", Input: agentInput(t, "hi"), Entries: store, Events: event.NewBroker("session", store),
	}

	for range 2 {
		if err := NewExecutor(factory).Execute(context.Background(), params); err != nil {
			t.Fatal(err)
		}
	}

	model.AssertCallCount(t, 1)

	entries, err := store.List(context.Background(), "session", 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("history after resume = %+v, %v", entries, err)
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
	broker := event.NewBroker("session-1", store)

	subscription, err := broker.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Close)

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: "execution-1", Input: agentInput(t, "hi"), Entries: store, Events: broker}
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
