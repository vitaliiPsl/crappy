package executor

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
)

func TestExecutorPersistsCompactionAndReceipt(t *testing.T) {
	t.Parallel()

	usage := kit.Usage{InputTokens: 10, OutputTokens: 2}
	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"saved context"}`)),
		FinishReason: kit.FinishReasonStop, Usage: usage,
	}})
	store := entry.NewMemoryStore()
	opened := newTestSession(t, model, store)

	original := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("older work")),
		kit.NewModelMessage(kit.NewTextContent("recent reply")),
	}
	if err := opened.Commit(context.Background(), "previous", agent.MessageEntryType, original[0], original[1]); err != nil {
		t.Fatal(err)
	}

	current, finished := executeCompaction(t, opened)
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

	memory := agent.NewMemory(opened.ID(), "next", store, event.NewBus(opened.ID(), store))

	history, err := memory.History(context.Background())
	if err != nil || !reflect.DeepEqual(history, original) {
		t.Fatalf("manual compaction changed original messages: %+v, err = %v", history, err)
	}

	messages, err := memory.Context(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []kit.Message{compaction.Compaction{Summary: compaction.Summary{Objective: "saved context"}}.Message(), original[1]}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("model context = %+v", messages)
	}

	reopened, err := session.New(context.Background(), opened.ID(), store, execution.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(reopened.Close)

	loaded, err := reopened.Execution(context.Background(), current.ID())
	if err != nil {
		t.Fatal(err)
	}

	restored, err := loaded.Wait(context.Background())
	if err != nil || !reflect.DeepEqual(restored, finished) {
		t.Fatalf("restored execution = %+v, err = %v", restored, err)
	}

	model.AssertCallCount(t, 1)
}

func TestExecutorSkipsEmptyConversation(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t)
	store := entry.NewMemoryStore()
	opened := newTestSession(t, model, store)

	_, finished := executeCompaction(t, opened)
	if finished.Outcome != execution.Completed {
		t.Fatalf("empty compaction = %+v", finished)
	}

	var receipt Result
	if err := json.Unmarshal(finished.Output, &receipt); err != nil {
		t.Fatal(err)
	}

	if receipt != (Result{}) {
		t.Fatalf("empty compaction receipt = %+v", receipt)
	}

	stored, err := store.List(context.Background(), opened.ID(), 0)
	if err != nil || len(stored) != 2 || stored[0].Type != execution.StartedEntryType || stored[1].Type != execution.FinishedEntryType {
		t.Fatalf("empty compaction journal = %+v, err = %v", stored, err)
	}

	model.AssertCallCount(t, 0)
}

func TestExecutorPreservesContextAndUsageOnFailure(t *testing.T) {
	t.Parallel()

	for _, failCommit := range []bool{false, true} {
		name, output := "generation", "invalid json"
		if failCommit {
			name, output = "commit", `{"objective":"saved context"}`
		}

		t.Run(name, func(t *testing.T) {
			usage := kit.Usage{InputTokens: 7, OutputTokens: 3}
			model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
				Message: kit.NewModelMessage(kit.NewTextContent(output)), FinishReason: kit.FinishReasonStop, Usage: usage,
			}})

			var store entry.Store = entry.NewMemoryStore()

			if failCommit {
				store = newFailingStore(store, errors.New("cannot save compaction"))
			}

			opened := newTestSession(t, model, store)

			original := kit.NewUserMessage(kit.NewTextContent("original work"))
			if err := opened.Commit(context.Background(), "previous", agent.MessageEntryType, original); err != nil {
				t.Fatal(err)
			}

			_, finished := executeCompaction(t, opened)
			if finished.Outcome != execution.Failed || finished.Error == "" {
				t.Fatalf("failed compaction = %+v", finished)
			}

			var receipt Result
			if err := json.Unmarshal(finished.Output, &receipt); err != nil {
				t.Fatal(err)
			}

			if receipt.Compacted || receipt.Usage != usage {
				t.Fatalf("failure receipt = %+v", receipt)
			}

			if _, found, err := store.Latest(context.Background(), opened.ID(), compaction.EntryType); err != nil || found {
				t.Fatalf("failed compaction changed active context: found = %v, err = %v", found, err)
			}

			memory := agent.NewMemory(opened.ID(), "next", store, event.NewBus(opened.ID(), store))

			messages, err := memory.Context(context.Background())
			if err != nil || !reflect.DeepEqual(messages, []kit.Message{original}) {
				t.Fatalf("context after failure = %+v, err = %v", messages, err)
			}
		})
	}
}

func TestExecutorCancellationAndSessionOrdering(t *testing.T) {
	t.Parallel()

	model := newBlockingModel()
	store := entry.NewMemoryStore()

	opened := newTestSession(t, model, store)
	if err := opened.Commit(context.Background(), "previous", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("work"))); err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execute(context.Background(), execution.Request{Executor: ExecutorName})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-model.started:
	case <-time.After(3 * time.Second):
		t.Fatal("compaction model did not start")
	}

	if _, err := opened.Execute(context.Background(), execution.Request{Executor: ExecutorName}); !errors.Is(err, session.ErrBusy) {
		t.Fatalf("concurrent compaction error = %v", err)
	}

	if err := opened.Cancel(context.Background(), current.ID()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	finished, err := current.Wait(ctx)
	if err != nil || finished.Outcome != execution.Cancelled {
		t.Fatalf("cancelled compaction = %+v, err = %v", finished, err)
	}

	var receipt Result
	if err := json.Unmarshal(finished.Output, &receipt); err != nil {
		t.Fatal(err)
	}

	if receipt.Compacted || receipt.Usage != (kit.Usage{InputTokens: 5}) {
		t.Fatalf("cancelled receipt = %+v", receipt)
	}

	if _, found, err := store.Latest(context.Background(), opened.ID(), compaction.EntryType); err != nil || found {
		t.Fatalf("cancelled compaction was committed: found = %v, err = %v", found, err)
	}
}

func TestLoadCompactionContextRejectsMalformedRecords(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{compaction.EntryType, agent.MessageEntryType} {
		t.Run(kind, func(t *testing.T) {
			store := entry.NewMemoryStore()
			if _, err := store.Append(context.Background(), "session", entry.Entry{Type: kind, Payload: json.RawMessage(`"invalid"`)}); err != nil {
				t.Fatal(err)
			}

			if _, _, err := loadCompactionContext(context.Background(), "session", store); err == nil || !strings.Contains(err.Error(), "entry 1") {
				t.Fatalf("loadCompactionContext() error = %v", err)
			}
		})
	}
}

func newTestSession(t *testing.T, model kit.Model, store entry.Store) *session.Session {
	t.Helper()

	compactor, err := compaction.NewCompactor(compaction.Config{}, newModelResolver(model))
	if err != nil {
		t.Fatal(err)
	}

	registry := execution.NewRegistry()
	registry.Register(ExecutorName, New(compactor))

	opened, err := session.New(context.Background(), "session", store, registry)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(opened.Close)

	return opened
}

func executeCompaction(t *testing.T, opened *session.Session) (*session.Execution, execution.Result) {
	t.Helper()

	current, err := opened.Execute(context.Background(), execution.Request{Executor: ExecutorName})
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

type modelResolver struct {
	model kit.Model
}

func newModelResolver(model kit.Model) *modelResolver {
	return &modelResolver{model: model}
}

func (r *modelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

type failingStore struct {
	entry.Store
	failure error
}

func newFailingStore(store entry.Store, failure error) *failingStore {
	return &failingStore{Store: store, failure: failure}
}

func (s *failingStore) Append(ctx context.Context, sessionID string, values ...entry.Entry) ([]entry.Entry, error) {
	for _, value := range values {
		if value.Type == compaction.EntryType {
			return nil, s.failure
		}
	}

	return s.Store.Append(ctx, sessionID, values...)
}

type blockingModel struct {
	started chan struct{}
}

func newBlockingModel() *blockingModel {
	return &blockingModel{started: make(chan struct{})}
}

func (*blockingModel) Config() kit.ModelConfig {
	return kit.ModelConfig{ID: "blocking"}
}

func (m *blockingModel) Generate(ctx context.Context, _ kit.ModelRequest) (kit.ModelResponse, error) {
	close(m.started)
	<-ctx.Done()

	return kit.ModelResponse{Usage: kit.Usage{InputTokens: 5}}, ctx.Err()
}

func (m *blockingModel) Stream(ctx context.Context, request kit.ModelRequest) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.StreamFromGenerate(ctx, request, m.Generate)
}
