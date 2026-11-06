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

func TestExecutorPersistsCompaction(t *testing.T) {
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

	current, status := execute(t, opened, execution.NewRequest(ExecutorName, nil))
	if status != execution.Completed {
		t.Fatalf("manual compaction = %q", status)
	}

	stored, err := store.List(context.Background(), opened.ID(), 0)
	if err != nil {
		t.Fatal(err)
	}

	wantTypes := []string{
		agent.MessageEntryType, agent.MessageEntryType,
		execution.EntryType, execution.EntryType, compaction.EntryType, execution.EntryType,
	}
	if len(stored) != len(wantTypes) {
		t.Fatalf("log contains %d entries, want %d", len(stored), len(wantTypes))
	}

	for i, value := range stored {
		if value.Type != wantTypes[i] || (i >= 2 && value.ExecutionID != current.ID()) {
			t.Fatalf("entry %d = %+v", i, value)
		}
	}

	if generated := decode[compaction.Compaction](t, stored[4].Payload); generated.Objective != "saved context" || len(generated.Retained) != 1 || generated.Usage != usage {
		t.Fatalf("compaction = %+v", generated)
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
	if err != nil || restored != status {
		t.Fatalf("restored execution = %+v, err = %v", restored, err)
	}

	model.AssertCallCount(t, 1)
}

func TestExecutorPreservesCompactionWhenResumedAfterCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	bus := event.NewBus("session", store)
	if err := bus.Commit(ctx, "work", compaction.EntryType, compaction.Compaction{
		Summary: compaction.Summary{Objective: "already compacted"},
	}); err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)

	executor := New(newCompactor(t, model))
	if err := executor.Execute(ctx, execution.ExecuteParams{
		SessionID: "session", ExecutionID: "work", Entries: store, Events: bus,
	}); err != nil {
		t.Fatal(err)
	}

	model.AssertNeverCalled(t)

	entries, err := store.List(ctx, "session", 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("resumed compaction journal = %+v, %v", entries, err)
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

	agentExecution, status := execute(t, opened, execution.NewRequest(
		agent.ExecutorName,
		kit.NewUserMessage(kit.NewTextContent("input longer than one token")),
	))
	if status != execution.Completed {
		t.Fatalf("execution with compactions = %q", status)
	}

	compactionModel.AssertCallCount(t, 1)

	if got, want := model.CallAt(0).Messages[0].TextContent().Text, compactionMessage("saved context"); got != want {
		t.Fatalf("agent request message = %q, want %q", got, want)
	}

	manualExecution, status := execute(t, opened, execution.NewRequest(ExecutorName, nil))
	if status != execution.Completed {
		t.Fatalf("manual compaction = %q", status)
	}

	model.AssertCallCount(t, 1)
	compactionModel.AssertCallCount(t, 2)

	stored, err := store.List(context.Background(), opened.ID(), 0)
	if err != nil {
		t.Fatal(err)
	}

	var startedIDs, finishedIDs, compactionIDs []string

	usage := make(map[string][]kit.Usage)

	for _, value := range stored {
		switch value.Type {
		case execution.EntryType:
			saved := decode[execution.Record](t, value.Payload)
			if saved.Status == execution.Running {
				startedIDs = append(startedIDs, value.ExecutionID)
			}

			if saved.Status.Terminal() {
				finishedIDs = append(finishedIDs, value.ExecutionID)
			}
		case compaction.EntryType:
			compactionIDs = append(compactionIDs, value.ExecutionID)
			usage[value.ExecutionID] = append(usage[value.ExecutionID], decode[compaction.Compaction](t, value.Payload).Usage)
		case agent.MessageEntryType:
			if message := decode[kit.Message](t, value.Payload); message.Usage != (kit.Usage{}) {
				usage[value.ExecutionID] = append(usage[value.ExecutionID], message.Usage)
			}
		}
	}

	want := []string{agentExecution.ID(), manualExecution.ID()}
	if !reflect.DeepEqual(startedIDs, want) || !reflect.DeepEqual(finishedIDs, want) || !reflect.DeepEqual(compactionIDs, want) {
		t.Fatalf("execution attribution: starts=%v, finishes=%v, compactions=%v", startedIDs, finishedIDs, compactionIDs)
	}

	wantUsage := map[string][]kit.Usage{
		agentExecution.ID():  {{InputTokens: 10, OutputTokens: 2}, {InputTokens: 4, OutputTokens: 1}},
		manualExecution.ID(): {{InputTokens: 5, OutputTokens: 1}},
	}
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Fatalf("usage = %+v, want compaction and agent usage recorded separately per execution: %+v", usage, wantUsage)
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

func execute(t *testing.T, opened *session.Session, request execution.Request) (*session.Execution, execution.Status) {
	t.Helper()

	current, err := opened.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	status, err := current.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return current, status
}

func decode[T any](t *testing.T, payload json.RawMessage) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}

	return value
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
