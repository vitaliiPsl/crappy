package executor

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/compaction/automatic"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestExecutorPersistsCompactionAndReceipt(t *testing.T) {
	t.Parallel()

	usage := kit.Usage{InputTokens: 10, OutputTokens: 2}
	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"saved context"}`)),
		FinishReason: kit.FinishReasonStop, Usage: usage,
	}})
	store := entry.NewMemoryStore()

	registry := execution.NewRegistry()
	registry.Register(ExecutorName, New(newCompactor(t, model)))
	opened := newTestSession(t, store, registry)

	if err := opened.Commit(context.Background(), "previous", agent.MessageEntryType,
		kit.NewUserMessage(kit.NewTextContent("older work")),
		kit.NewModelMessage(kit.NewTextContent("recent reply")),
	); err != nil {
		t.Fatal(err)
	}

	current, finished := execute(t, opened, execution.Request{Executor: ExecutorName})
	if finished.Outcome != execution.Completed {
		t.Fatalf("manual compaction = %+v", finished)
	}

	var receipt Result
	if err := json.Unmarshal(finished.Output, &receipt); err != nil {
		t.Fatal(err)
	}

	if !receipt.Compacted || receipt.Usage != usage || receipt.RetainedMessages != 1 || receipt.TokensBefore <= 0 || receipt.TokensAfter <= 0 {
		t.Fatalf("receipt = %+v", receipt)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(finished.Output, &fields); err != nil {
		t.Fatal(err)
	}

	if fields["objective"] != nil || fields["retained"] != nil {
		t.Fatalf("execution output duplicates compaction content: %s", finished.Output)
	}

	stored, err := store.List(context.Background(), opened.ID(), 0)
	if err != nil {
		t.Fatal(err)
	}

	wantTypes := []string{agent.MessageEntryType, agent.MessageEntryType, execution.StartedEntryType, compaction.EntryType, execution.FinishedEntryType}
	if len(stored) != len(wantTypes) {
		t.Fatalf("journal contains %d entries, want %d", len(stored), len(wantTypes))
	}

	for i, value := range stored {
		if value.Type != wantTypes[i] || (i >= 2 && value.ExecutionID != current.ID()) {
			t.Fatalf("entry %d = %+v", i, value)
		}
	}

	var started execution.Started
	if err := json.Unmarshal(stored[2].Payload, &started); err != nil {
		t.Fatal(err)
	}

	if started.Executor != ExecutorName {
		t.Fatalf("executor = %q", started.Executor)
	}

	reopened, err := session.New(context.Background(), opened.ID(), opened.State(), store, execution.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(reopened.Close)

	reloaded, err := reopened.Execution(context.Background(), current.ID())
	if err != nil {
		t.Fatal(err)
	}

	restored, err := reloaded.Wait(context.Background())
	if err != nil || !reflect.DeepEqual(restored, finished) {
		t.Fatalf("restored execution = %+v, err = %v", restored, err)
	}

	model.AssertCallCount(t, 1)
}

func TestExecutorReportsUsageOnFailure(t *testing.T) {
	t.Parallel()

	usage := kit.Usage{InputTokens: 7, OutputTokens: 3}
	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message: kit.NewModelMessage(kit.NewTextContent("invalid json")), FinishReason: kit.FinishReasonStop, Usage: usage,
	}})
	store := entry.NewMemoryStore()
	bus := event.NewBus("session", store)

	if err := bus.Commit(context.Background(), "previous", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("original work"))); err != nil {
		t.Fatal(err)
	}

	params := execution.ExecuteParams{SessionID: "session", ExecutionID: "execution", Entries: store, Events: bus}

	output, err := New(newCompactor(t, model)).Execute(context.Background(), params)
	if err == nil {
		t.Fatal("Execute() error = nil, want generation failure")
	}

	if receipt := output.(Result); receipt.Compacted || receipt.Usage != usage {
		t.Fatalf("failure receipt = %+v", receipt)
	}
}

func TestManualAndAutomaticCompactionRecordToTheirExecutions(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent("done")),
		FinishReason: kit.FinishReasonStop,
		Usage:        kit.Usage{InputTokens: 4, OutputTokens: 1},
	}})
	compactionModel := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"saved context"}`)),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 10, OutputTokens: 2},
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"manual compaction"}`)),
			FinishReason: kit.FinishReasonStop,
			Usage:        kit.Usage{InputTokens: 5, OutputTokens: 1},
		}},
	)
	compactor := newCompactor(t, compactionModel)
	store := entry.NewMemoryStore()

	registry := execution.NewRegistry()
	registry.Register(ExecutorName, New(compactor))
	registry.Register(agent.ExecutorName, agent.NewExecutor(agent.New(kit.AgentConfig{}, staticModelResolver{model},
		agent.WithContributors(automatic.New(compactor, automatic.WithTrigger(automatic.WhenInputExceedsTokens(1)))),
	)))
	opened := newTestSession(t, store, registry)

	agentExecution, finished := execute(t, opened, execution.Request{
		Executor: agent.ExecutorName,
		Input:    kit.NewUserMessage(kit.NewTextContent("input longer than one token")),
	})
	if finished.Outcome != execution.Completed {
		t.Fatalf("execution with compactions = %+v", finished)
	}

	compactionModel.AssertCallCount(t, 1)

	if got, want := model.CallAt(0).Messages[0].TextContent().Text, compactionMessage("saved context"); got != want {
		t.Fatalf("agent request message = %q, want %q", got, want)
	}

	manualExecution, finished := execute(t, opened, execution.Request{Executor: ExecutorName})
	if finished.Outcome != execution.Completed {
		t.Fatalf("manual compaction = %+v", finished)
	}

	var receipt Result
	if err := json.Unmarshal(finished.Output, &receipt); err != nil {
		t.Fatal(err)
	}

	if !receipt.Compacted || receipt.Usage != (kit.Usage{InputTokens: 5, OutputTokens: 1}) {
		t.Fatalf("manual receipt = %+v", receipt)
	}

	model.AssertCallCount(t, 1)
	compactionModel.AssertCallCount(t, 2)

	stored, err := store.List(context.Background(), opened.ID(), 0)
	if err != nil {
		t.Fatal(err)
	}

	var startedIDs, finishedIDs, compactionIDs []string

	for _, value := range stored {
		switch value.Type {
		case execution.StartedEntryType:
			startedIDs = append(startedIDs, value.ExecutionID)
		case execution.FinishedEntryType:
			finishedIDs = append(finishedIDs, value.ExecutionID)
		case compaction.EntryType:
			compactionIDs = append(compactionIDs, value.ExecutionID)
		case agent.FinishedEntryType:
			var result agent.Finished
			if err := json.Unmarshal(value.Payload, &result); err != nil {
				t.Fatal(err)
			}

			if result.Usage != (kit.Usage{InputTokens: 14, OutputTokens: 3}) {
				t.Fatalf("agent usage = %+v, want model and compaction usage", result.Usage)
			}
		}
	}

	want := []string{agentExecution.ID(), manualExecution.ID()}
	if !reflect.DeepEqual(startedIDs, want) || !reflect.DeepEqual(finishedIDs, want) || !reflect.DeepEqual(compactionIDs, want) {
		t.Fatalf("execution attribution: starts=%v, finishes=%v, compactions=%v", startedIDs, finishedIDs, compactionIDs)
	}
}

func newTestSession(t *testing.T, store entry.Store, executors execution.Resolver) *session.Session {
	t.Helper()

	sessions := state.NewMemoryStore()
	if err := sessions.Save(context.Background(), state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	opened, err := session.New(context.Background(), "session", state.New(sessions, "session"), store, executors)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(opened.Close)

	return opened
}

func execute(t *testing.T, opened *session.Session, request execution.Request) (*session.Execution, execution.Result) {
	t.Helper()

	current, err := opened.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	finished, err := current.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return current, finished
}

func newCompactor(t *testing.T, model kit.Model) *compaction.Compactor {
	t.Helper()

	compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{model})
	if err != nil {
		t.Fatal(err)
	}

	return compactor
}

func compactionMessage(objective string) string {
	return compaction.Compaction{Summary: compaction.Summary{Objective: objective}}.Message().TextContent().Text
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
