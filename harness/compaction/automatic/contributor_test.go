package automatic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	harnessagent "github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestContributorCompactsBeforeModelRequest(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 10, OutputTokens: 2},
		}},
	)
	compactionModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"compaction"}`)),
		FinishReason: kit.FinishReasonStop,
		Usage:        kit.Usage{InputTokens: 50, OutputTokens: 5},
	}})
	built := buildAgent(t, model, compactionModel, WithEstimator(fixedEstimator{request: 80}))

	response, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original")))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := response.Usage, (kit.Usage{InputTokens: 60, OutputTokens: 7}); got != want {
		t.Fatalf("response usage = %#v, want %#v", got, want)
	}

	if got, want := response.LastUsage, (kit.Usage{InputTokens: 10, OutputTokens: 2}); got != want {
		t.Fatalf("response last usage = %#v, want %#v", got, want)
	}

	compactionModel.AssertCallCount(t, 1)

	messages := model.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, compactionMessage("compaction"); got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}
}

func TestContributorCompactsAndRetriesOnContextOverflow(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Error: kit.ErrContextLength},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	compactionModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"compaction"}`)),
		FinishReason: kit.FinishReasonStop,
	}})
	built := buildAgent(t, model, compactionModel, WithEstimator(fixedEstimator{}))

	if _, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original"))); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	compactionModel.AssertCallCount(t, 1)
	model.AssertCallCount(t, 2)

	if got, want := model.CallAt(1).Messages[0].TextContent().Text, compactionMessage("compaction"); got != want {
		t.Fatalf("retried request message = %q, want %q", got, want)
	}
}

func TestContributorIgnoresOtherModelErrors(t *testing.T) {
	t.Parallel()

	model := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 100},
		kittest.ModelResult{Error: kit.ErrRateLimit},
	)
	compactionModel := kittest.NewModel(t)
	built := buildAgent(t, model, compactionModel, WithEstimator(fixedEstimator{}))

	_, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original")))
	if !errors.Is(err, kit.ErrRateLimit) {
		t.Fatalf("Run() error = %v, want ErrRateLimit", err)
	}

	model.AssertCallCount(t, 1)
	compactionModel.AssertCallCount(t, 0)
}

func TestContributorUsesPreviousCompaction(t *testing.T) {
	t.Parallel()

	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("overflow=%v", overflow), func(t *testing.T) {
			ctx := context.Background()
			store := entry.NewMemoryStore()
			events := event.NewBus("session", store)
			retained := kit.NewModelMessage(kit.NewTextContent("retained evidence"))

			previous := compaction.Compaction{
				Summary: compaction.Summary{
					Objective: "previous objective", Progress: []string{"done: research"},
					Context: []string{"file: /repo/main.go"}, Next: []string{"implement"},
				},
				Retained: []kit.Message{retained}, Usage: kit.Usage{InputTokens: 900},
			}
			if err := events.Commit(ctx, "previous", harnessagent.MessageEntryType,
				kit.NewUserMessage(kit.NewTextContent("already summarized history")), retained,
			); err != nil {
				t.Fatal(err)
			}

			if err := events.Commit(ctx, "previous", compaction.EntryType,
				compaction.Compaction{Summary: compaction.Summary{Objective: "superseded compaction"}}, previous,
			); err != nil {
				t.Fatal(err)
			}

			if err := events.Commit(ctx, "previous", "execution.note", "metadata"); err != nil {
				t.Fatal(err)
			}

			results := []kittest.ModelResult{{Response: kit.ModelResponse{
				Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
				Usage: kit.Usage{InputTokens: 1, OutputTokens: 1},
			}}}

			threshold := int64(1)
			if overflow {
				threshold = 0

				results = append([]kittest.ModelResult{{Error: kit.ErrContextLength}}, results...)
			}

			model := kittest.NewModel(t, results...)
			summaryModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
				Message: kit.NewModelMessage(kit.NewTextContent(`{"objective":"updated objective"}`)), FinishReason: kit.FinishReasonStop,
				Usage: kit.Usage{InputTokens: 10, OutputTokens: 2},
			}})
			retainNothing := 0

			compactor, err := compaction.NewCompactor(compaction.Config{RetainedTokens: &retainNothing}, staticModelResolver{summaryModel})
			if err != nil {
				t.Fatal(err)
			}

			factory := harnessagent.New(kit.AgentConfig{}, staticModelResolver{model},
				harnessagent.WithContributors(New(compactionexec.New(compactor), WithTrigger(WhenInputExceedsTokens(threshold)))),
			)
			memory := harnessagent.NewMemory("session", "current", store, events)

			built, err := factory.Build(ctx, harnessagent.BuildRequest{
				SessionID: "session", ExecutionID: "current", Memory: memory, Entries: store, Events: events,
			})
			if err != nil {
				t.Fatal(err)
			}

			input := kit.NewUserMessage(kit.NewTextContent("new work"))

			response, err := built.Run(ctx, input)
			if err != nil {
				t.Fatal(err)
			}

			summaryModel.AssertCallCount(t, 1)
			model.AssertCallCount(t, len(results))

			prompt := summaryModel.CallAt(0).Messages[0].TextContent().Text

			_, previousJSON, found := strings.Cut(prompt, "<previous_compaction>\n")
			if !found {
				t.Fatalf("previous compaction missing: %q", prompt)
			}

			previousJSON, _, found = strings.Cut(previousJSON, "\n</previous_compaction>")

			var supplied compaction.Summary
			if err := json.Unmarshal([]byte(previousJSON), &supplied); err != nil || !found || !reflect.DeepEqual(supplied, previous.Summary) {
				t.Fatalf("supplied summary = %+v, err = %v", supplied, err)
			}

			if strings.Count(prompt, "retained evidence") != 1 || strings.Count(prompt, "new work") != 1 ||
				strings.Contains(prompt, "already summarized history") || strings.Contains(prompt, "superseded compaction") ||
				strings.Contains(prompt, previous.Message().TextContent().Text) {
				t.Fatalf("incorrect compaction input: %q", prompt)
			}

			if response.Usage != (kit.Usage{InputTokens: 11, OutputTokens: 3}) {
				t.Fatalf("usage includes previous compaction generation: %+v", response.Usage)
			}

			request := model.CallAt(len(results) - 1)
			if len(request.Messages) != 1 || request.Messages[0].TextContent().Text != compactionMessage("updated objective") {
				t.Fatalf("request did not use the new compaction: %+v", request.Messages)
			}

			history, err := memory.History(ctx)
			if err != nil || len(history) != 4 {
				t.Fatalf("original history changed: %+v, err = %v", history, err)
			}
		})
	}
}

func TestContributorCompactsPreviousCompactionAgainOnOverflow(t *testing.T) {
	t.Parallel()
	model := kittest.NewModel(t,
		kittest.ModelResult{Error: kit.ErrContextLength},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
		}},
	)
	summaryModel := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent(`{"objective":"first summary"}`)), FinishReason: kit.FinishReasonStop,
			Usage: kit.Usage{InputTokens: 5, OutputTokens: 1},
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent(`{"objective":"second summary"}`)), FinishReason: kit.FinishReasonStop,
			Usage: kit.Usage{InputTokens: 5, OutputTokens: 1},
		}},
	)
	store := entry.NewMemoryStore()
	events := event.NewBus("session", store)
	retainNothing := 0

	compactor, err := compaction.NewCompactor(compaction.Config{RetainedTokens: &retainNothing}, staticModelResolver{summaryModel})
	if err != nil {
		t.Fatal(err)
	}

	factory := harnessagent.New(kit.AgentConfig{}, staticModelResolver{model},
		harnessagent.WithContributors(New(compactionexec.New(compactor), WithTrigger(WhenInputExceedsTokens(1)))),
	)

	built, err := factory.Build(context.Background(), harnessagent.BuildRequest{
		SessionID: "session", ExecutionID: "execution", Entries: store, Events: events,
		Memory: harnessagent.NewMemory("session", "execution", store, events),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original work")))
	if err != nil {
		t.Fatal(err)
	}

	summaryModel.AssertCallCount(t, 2)
	model.AssertCallCount(t, 2)

	prompt := summaryModel.CallAt(1).Messages[0].TextContent().Text
	if !strings.Contains(prompt, `"objective": "first summary"`) || strings.Contains(prompt, "original work") ||
		strings.Contains(prompt, compactionMessage("first summary")) {
		t.Fatalf("second compaction input = %q", prompt)
	}

	if response.Usage != (kit.Usage{InputTokens: 10, OutputTokens: 2}) {
		t.Fatalf("usage = %+v", response.Usage)
	}

	if got := model.CallAt(1).Messages[0].TextContent().Text; got != compactionMessage("second summary") {
		t.Fatalf("retried request message = %q", got)
	}
}

func buildAgent(t *testing.T, model, compactionModel kit.Model, options ...ContributorOption) kit.Agent {
	t.Helper()

	compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{compactionModel})
	if err != nil {
		t.Fatal(err)
	}

	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	factory := harnessagent.New(
		kit.AgentConfig{},
		staticModelResolver{model},
		harnessagent.WithContributors(New(compactionexec.New(compactor), options...)),
	)

	built, err := factory.Build(context.Background(), harnessagent.BuildRequest{
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Memory:      harnessagent.NewMemory("session-1", "execution-1", store, bus),
		Entries:     store,
		Events:      bus,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	return built
}

func TestContributorUsesLastModelUsage(t *testing.T) {
	t.Parallel()

	contributor := New(nil, WithEstimator(fixedEstimator{request: 1000, message: 7}))
	run := &kit.RunContext{
		LastUsage: kit.Usage{InputTokens: 100, OutputTokens: 20},
		Messages: []kit.Message{
			kit.NewModelMessage(kit.NewTextContent("tool call")),
			kit.NewToolMessage(kit.NewTextContent("tool result")),
		},
	}

	if got, want := contributor.inputTokens(run, kit.ModelRequest{}), int64(127); got != want {
		t.Fatalf("inputTokens() = %d, want %d", got, want)
	}
}

type fixedEstimator struct {
	request int
	message int
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

func (e fixedEstimator) EstimateRequest(kit.ModelRequest) int {
	return e.request
}

func (e fixedEstimator) EstimateMessage(kit.Message) int {
	return e.message
}

func TestAutomaticCompactionDuringExecution(t *testing.T) {
	t.Parallel()

	agentModel := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 1},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	compactionModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective": "compacted context", "progress": [], "context": [], "next": []}`)),
		FinishReason: kit.FinishReasonStop,
	}})

	compactor, err := compaction.NewCompactor(
		compaction.Config{},
		staticModelResolver{compactionModel},
	)
	if err != nil {
		t.Fatalf("NewCompactor() error = %v", err)
	}

	store := entry.NewMemoryStore()
	bus := event.NewBus("session-1", store)
	executor := harnessagent.NewExecutor(harnessagent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, harnessagent.WithContributors(New(compactionexec.New(compactor))),
	))

	subscription, err := bus.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	executionID := "execution-1"
	done := make(chan error, 1)

	params := execution.ExecuteParams{SessionID: "session-1", ExecutionID: executionID, Input: agentInput(t, "start"), Entries: store, Events: bus}
	go func() {
		_, err := executor.Execute(context.Background(), params)
		done <- err
	}()

	started := false
	for {
		received := receiveEvent(t, subscription)
		if received.Type == compaction.StartedEventType {
			started = received.ExecutionID == executionID
		}

		if received.Type == compaction.EntryType {
			break
		}
	}

	if !started {
		t.Fatal("compaction started event was not emitted before the compaction entry")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	messages := agentModel.CallAt(0).Messages
	if got, want := len(messages), 1; got != want {
		t.Fatalf("request messages = %d, want %d", got, want)
	}

	if got, want := messages[0].TextContent().Text, compactionMessage("compacted context"); got != want {
		t.Fatalf("request message = %q, want %q", got, want)
	}

	entries, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(entries) < 3 {
		t.Fatalf("entries = %d, want at least 3", len(entries))
	}

	if got, want := entries[1].Type, compaction.EntryType; got != want {
		t.Fatalf("entry 3 type = %q, want %q", got, want)
	}

	if got := entries[1].ExecutionID; got != executionID {
		t.Fatalf("compaction ExecutionID = %q, want %q", got, executionID)
	}
}

func TestAutomaticCompactionRetainsCurrentInput(t *testing.T) {
	t.Parallel()

	agentModel := kittest.NewModelWithConfig(
		t,
		kit.ModelConfig{InputLimit: 1},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	compactionModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective": "compacted context", "progress": [], "context": [], "next": []}`)),
		FinishReason: kit.FinishReasonStop,
	}})

	compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{compactionModel})
	if err != nil {
		t.Fatalf("NewCompactor() error = %v", err)
	}

	store := entry.NewMemoryStore()
	if err := event.NewBus("session-1", store).Commit(
		context.Background(), "", harnessagent.MessageEntryType,
		kit.NewUserMessage(kit.NewTextContent("earlier")),
		kit.NewModelMessage(kit.NewTextContent("earlier response")),
	); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	executor := harnessagent.NewExecutor(harnessagent.New(
		kit.AgentConfig{}, staticModelResolver{agentModel}, harnessagent.WithContributors(New(compactionexec.New(compactor))),
	))
	if _, err := executor.Execute(context.Background(), execution.ExecuteParams{
		SessionID: "session-1", ExecutionID: "execution-1", Input: agentInput(t, "current"), Entries: store, Events: event.NewBus("session-1", store),
	}); err != nil {
		t.Fatal(err)
	}

	messages := agentModel.CallAt(0).Messages
	want := []string{compactionMessage("compacted context"), "earlier response", "current"}

	if got := len(messages); got != len(want) {
		t.Fatalf("request messages = %d, want %d", got, len(want))
	}

	for i, text := range want {
		if got := messages[i].TextContent().Text; got != text {
			t.Fatalf("request message %d = %q, want %q", i, got, text)
		}
	}
}

func compactionMessage(objective string) string {
	return compaction.Compaction{Summary: compaction.Summary{Objective: objective}}.Message().TextContent().Text
}

func receiveEvent(t *testing.T, subscription *event.Subscription) event.Event {
	t.Helper()

	select {
	case value, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")

		return event.Event{}
	}
}

func agentInput(t *testing.T, text string) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(text)))
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

func TestCompactionCommitFailurePreservesContextAndUsage(t *testing.T) {
	t.Parallel()

	failure := errors.New("cannot save compaction")
	store := &compactionFailingStore{Store: entry.NewMemoryStore(), failure: failure}
	bus := event.NewBus("session", store)
	mainModel := kittest.NewModelWithConfig(t, kit.ModelConfig{InputLimit: 1})
	summaryModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"summary"}`)),
		FinishReason: kit.FinishReasonStop, Usage: kit.Usage{InputTokens: 10, OutputTokens: 2},
	}})

	compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{summaryModel})
	if err != nil {
		t.Fatal(err)
	}

	factory := harnessagent.New(kit.AgentConfig{}, staticModelResolver{mainModel}, harnessagent.WithContributors(New(compactionexec.New(compactor))))

	built, err := factory.Build(context.Background(), harnessagent.BuildRequest{SessionID: "session", ExecutionID: "execution", Memory: harnessagent.NewMemory("session", "execution", store, bus), Entries: store, Events: bus})
	if err != nil {
		t.Fatal(err)
	}

	response, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original")))
	if !errors.Is(err, failure) {
		t.Fatalf("Run() error=%v", err)
	}

	if response.Usage != (kit.Usage{InputTokens: 10, OutputTokens: 2}) {
		t.Fatalf("usage lost: %+v", response.Usage)
	}

	mainModel.AssertCallCount(t, 0)

	messages, err := harnessagent.NewMemory("session", "execution", store, bus).Context(context.Background())
	if err != nil || len(messages) != 1 || messages[0].TextContent().Text != "original" {
		t.Fatalf("failed compaction changed context: %+v, err=%v", messages, err)
	}
}

type compactionFailingStore struct {
	entry.Store
	failure error
}

func (s *compactionFailingStore) Append(ctx context.Context, sessionID string, values ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range values {
		if value.Type == compaction.EntryType {
			return nil, s.failure
		}
	}

	return s.Store.Append(ctx, sessionID, values...)
}

func TestCompactionRebuildsRequestContributions(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name    string
		prefix  string
		rewrite bool
	}{
		{name: "distinct contributions", prefix: "transient prefix"},
		{name: "identical transient prefix", prefix: "original"},
		{name: "rewritten conversation", prefix: "transient prefix", rewrite: true},
	}
	for _, scenario := range scenarios {
		for _, overflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/overflow=%v", scenario.name, overflow), func(t *testing.T) {
				prefix := kit.NewUserMessage(kit.NewTextContent(scenario.prefix))
				suffix := kit.NewUserMessage(kit.NewTextContent("transient suffix"))
				results := []kittest.ModelResult{{Response: kit.ModelResponse{Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop}}}

				inputLimit := 1
				if overflow {
					inputLimit = 100000

					results = append([]kittest.ModelResult{{Error: kit.ErrContextLength}}, results...)
				}

				model := kittest.NewModelWithConfig(t, kit.ModelConfig{InputLimit: inputLimit}, results...)
				summaryModel := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{Message: kit.NewModelMessage(kit.NewTextContent(`{"objective":"summary"}`)), FinishReason: kit.FinishReasonStop}})

				compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{summaryModel})
				if err != nil {
					t.Fatal(err)
				}

				store := entry.NewMemoryStore()
				events := event.NewBus("session", store)
				constructions := 0
				factory := harnessagent.New(kit.AgentConfig{Instructions: "static"}, staticModelResolver{model},
					harnessagent.WithAgentOptions(adkagent.WithOnModelRequest(func(_ *kit.RunContext, request kit.ModelRequest) (kit.ModelRequest, error) {
						constructions++

						if scenario.rewrite {
							rewritten := make([]kit.Message, len(request.Messages))
							for i, message := range request.Messages {
								rewritten[i] = kit.NewUserMessage(kit.NewTextContent("wrapped: " + message.TextContent().Text))
							}

							request.Messages = rewritten
						}

						request.Messages = append([]kit.Message{prefix}, request.Messages...)
						request.Instructions += " dynamic"

						return request, nil
					})),
					harnessagent.WithContributors(New(compactionexec.New(compactor)), harnessagent.ContributorFunc(func(context.Context, harnessagent.BuildContext) (harnessagent.Contribution, error) {
						return harnessagent.Contribution{Options: []adkagent.Option{
							adkagent.WithOnModelRequest(func(_ *kit.RunContext, request kit.ModelRequest) (kit.ModelRequest, error) {
								request.Messages = append(request.Messages, suffix)

								return request, nil
							}),
						}}, nil
					})),
				)

				built, err := factory.Build(context.Background(), harnessagent.BuildRequest{SessionID: "session", ExecutionID: "execution", Memory: harnessagent.NewMemory("session", "execution", store, events), Entries: store, Events: events})
				if err != nil {
					t.Fatal(err)
				}

				if _, err := built.Run(context.Background(), kit.NewUserMessage(kit.NewTextContent("original"))); err != nil {
					t.Fatal(err)
				}

				call := 0
				if overflow {
					call = 1
				}

				request := model.CallAt(call)

				summary := compactionMessage("summary")
				if scenario.rewrite {
					summary = "wrapped: " + summary
				}

				if len(request.Messages) != 3 || request.Messages[0].TextContent().Text != scenario.prefix || request.Messages[1].TextContent().Text != summary || request.Messages[2].TextContent().Text != "transient suffix" {
					t.Fatalf("request contributions lost or duplicated: %+v", request.Messages)
				}

				if request.Instructions != "static dynamic" || constructions != 2 {
					t.Fatalf("instructions=%q, constructions=%d", request.Instructions, constructions)
				}

				summaryModel.AssertCallCount(t, 1)

				summaryInput := summaryModel.CallAt(0).Messages[0].TextContent().Text
				if strings.Contains(summaryInput, "transient") || strings.Contains(summaryInput, "wrapped:") {
					t.Fatalf("transient contributions leaked into compaction: %q", summaryInput)
				}

				original, err := harnessagent.NewMemory("session", "execution", store, events).History(context.Background())
				if err != nil || len(original) != 2 || original[0].TextContent().Text != "original" {
					t.Fatalf("transient messages leaked into history: %+v, err=%v", original, err)
				}
			})
		}
	}
}
