package agent

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

	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestHistoryAppendRecordsExecutionAttribution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	history := NewHistory("session", "execution", store, event.NewBroker("session", store))
	if err := history.Append(ctx, kit.NewUserMessage(kit.NewTextContent("hello")), kit.NewModelMessage(kit.NewTextContent("hi"))); err != nil {
		t.Fatal(err)
	}

	stored, err := store.List(ctx, "session", 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(stored) != 2 {
		t.Fatalf("recorded %d entries", len(stored))
	}

	for _, value := range stored {
		if value.ExecutionID != "execution" || value.Type != MessageEntryType {
			t.Fatalf("unexpected message entry: %+v", value)
		}
	}
}

func TestHistoryMessagesProjectsLatestCompaction(t *testing.T) {
	t.Parallel()

	first := kit.NewUserMessage(kit.NewTextContent("first"))
	retained := kit.NewModelMessage(kit.NewTextContent("retained"))
	summary := compaction.Compaction{Summary: compaction.Summary{Objective: "summary"}}.Message()
	newSummary := compaction.Compaction{Summary: compaction.Summary{Objective: "new summary"}}.Message()
	later := kit.NewUserMessage(kit.NewTextContent("later"))
	unmeasured := kit.NewModelMessage(kit.NewTextContent("measured"))
	measured := unmeasured
	measured.Usage = kit.Usage{InputTokens: 40, OutputTokens: 4}

	tests := []struct {
		name    string
		entries []entry.Entry
		want    []kit.Message
	}{
		{"ordinary messages", []entry.Entry{messageEntry(t, first), messageEntry(t, retained)}, []kit.Message{first, retained}},
		{"summary and retained tail", []entry.Entry{messageEntry(t, first), messageEntry(t, retained), compactionEntry(t, "summary", retained), messageEntry(t, later)}, []kit.Message{summary, retained, later}},
		{"latest compaction", []entry.Entry{messageEntry(t, first), compactionEntry(t, "summary"), messageEntry(t, later), compactionEntry(t, "new summary", later)}, []kit.Message{newSummary, later}},
		{"no retained messages", []entry.Entry{messageEntry(t, first), compactionEntry(t, "summary"), messageEntry(t, later)}, []kit.Message{summary, later}},
		{"retained usage dropped", []entry.Entry{messageEntry(t, measured), compactionEntry(t, "summary", measured), messageEntry(t, measured)}, []kit.Message{summary, unmeasured, measured}},
		{"metadata has no effect", []entry.Entry{messageEntry(t, first), {Type: "execution.finished", Payload: json.RawMessage(`{}`)}}, []kit.Message{first}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			store := entry.NewMemoryStore()
			if _, err := store.Append(ctx, "session", test.entries...); err != nil {
				t.Fatal(err)
			}

			messages, err := NewHistory("session", "execution", store, event.NewBroker("session", store)).Messages(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(messages, test.want) {
				t.Fatalf("Messages() = %#v, want %#v", messages, test.want)
			}
		})
	}
}

func TestHistoryMessagesReturnsFreshValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()

	if _, err := store.Append(ctx, "session", messageEntry(t, kit.NewUserMessage(kit.NewTextContent("first")))); err != nil {
		t.Fatal(err)
	}

	history := NewHistory("session", "execution", store, event.NewBroker("session", store))

	messages, err := history.Messages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	messages[0].Content[0].Text.Text = "modified"

	again, err := history.Messages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if again[0].TextContent().Text != "first" {
		t.Fatalf("modifying read messages affected later reads: %+v", again)
	}
}

func TestHistoryMessagesRejectsMalformedRecords(t *testing.T) {
	t.Parallel()

	for _, value := range []entry.Entry{
		{Type: compaction.EntryType, Payload: json.RawMessage(`{"objective":"summary","retained":{}}`)},
		{Type: compaction.EntryType, Payload: json.RawMessage(`bad json`)},
		{Type: MessageEntryType, Payload: json.RawMessage(`bad json`)},
	} {
		ctx := context.Background()

		store := entry.NewMemoryStore()
		if _, err := store.Append(ctx, "session", value); err != nil {
			t.Fatal(err)
		}

		_, err := NewHistory("session", "execution", store, event.NewBroker("session", store)).Messages(ctx)
		if err == nil || !strings.Contains(err.Error(), "entry 1") {
			t.Fatalf("%s %s: error = %v", value.Type, value.Payload, err)
		}
	}
}

func TestHistoryReconstructsCompactedMessagesAfterReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	first := kit.NewUserMessage(kit.NewTextContent("first"))
	retained := kit.NewModelMessage(kit.NewTextContent("retained"))

	later := kit.NewUserMessage(kit.NewTextContent("later"))
	if _, err := storage.EntryStore().Append(ctx, "session", messageEntry(t, first), messageEntry(t, retained), compactionEntry(t, "summary", retained), messageEntry(t, later)); err != nil {
		t.Fatal(err)
	}

	reopened, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	store := reopened.EntryStore()

	messages, err := NewHistory("session", "next", store, event.NewBroker("session", store)).Messages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := []kit.Message{compaction.Compaction{Summary: compaction.Summary{Objective: "summary"}}.Message(), retained, later}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("reopened history = %#v, want %#v", messages, want)
	}
}

func TestHistoryCompactCommitsCompactionForExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := entry.NewMemoryStore()
	broker := event.NewBroker("session", store)
	original := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("older work")),
		kit.NewModelMessage(kit.NewTextContent("recent reply")),
	}

	if err := broker.Commit(ctx, "previous", MessageEntryType, original[0], original[1]); err != nil {
		t.Fatal(err)
	}

	subscription, err := broker.Subscribe(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Close)

	usage := kit.Usage{InputTokens: 10, OutputTokens: 2}
	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(`{"objective":"saved context"}`)),
		FinishReason: kit.FinishReasonStop,
		Usage:        usage,
	}})
	history := NewHistory("session", "execution", store, broker)

	if compacted, err := history.Compact(ctx, newCompactor(t, model)); err != nil || !compacted {
		t.Fatalf("Compact() = %v, error = %v", compacted, err)
	}

	for _, want := range []string{compaction.StartedEventType, compaction.EntryType} {
		received := receiveEvent(t, subscription)
		if received.Type != want || received.ExecutionID != "execution" {
			t.Fatalf("event = %+v, want %s from execution", received, want)
		}
	}

	var generated compaction.Compaction

	stored, found, err := store.Latest(ctx, "session", compaction.EntryType)
	if err != nil || !found || json.Unmarshal(stored.Payload, &generated) != nil || generated.Usage != usage {
		t.Fatalf("compaction entry = %+v, found = %v, error = %v, want usage %+v", stored, found, err, usage)
	}

	messages, err := history.Messages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := []kit.Message{compaction.Compaction{Summary: compaction.Summary{Objective: "saved context"}}.Message(), original[1]}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("Messages() = %#v, want %#v", messages, want)
	}
}

func TestHistoryCompactSkipsEmptyHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	model := kittest.NewModel(t)
	store := entry.NewMemoryStore()

	compacted, err := NewHistory("session", "execution", store, event.NewBroker("session", store)).Compact(ctx, newCompactor(t, model))
	if err != nil || compacted {
		t.Fatalf("Compact() = %v, error = %v", compacted, err)
	}

	if stored, err := store.List(ctx, "session", 0); err != nil || len(stored) != 0 {
		t.Fatalf("empty compaction recorded %+v, error = %v", stored, err)
	}

	model.AssertNeverCalled(t)
}

func TestHistoryCompactKeepsHistoryOnFailure(t *testing.T) {
	t.Parallel()

	for _, failCommit := range []bool{false, true} {
		name, output := "generation", "invalid json"
		if failCommit {
			name, output = "commit", `{"objective":"saved context"}`
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			usage := kit.Usage{InputTokens: 7, OutputTokens: 3}
			model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
				Message: kit.NewModelMessage(kit.NewTextContent(output)), FinishReason: kit.FinishReasonStop, Usage: usage,
			}})

			store := &failingStore{Store: entry.NewMemoryStore(), failure: errors.New("cannot save compaction")}
			if failCommit {
				store.failingType = compaction.EntryType
			}

			history := NewHistory("session", "execution", store, event.NewBroker("session", store))

			original := kit.NewUserMessage(kit.NewTextContent("original work"))
			if err := history.Append(ctx, original); err != nil {
				t.Fatal(err)
			}

			if compacted, err := history.Compact(ctx, newCompactor(t, model)); err == nil || compacted {
				t.Fatalf("Compact() = %v, error = %v", compacted, err)
			}

			if _, found, err := store.Latest(ctx, "session", compaction.EntryType); err != nil || found {
				t.Fatalf("failed compaction was recorded: found = %v, error = %v", found, err)
			}

			if messages, err := history.Messages(ctx); err != nil || !reflect.DeepEqual(messages, []kit.Message{original}) {
				t.Fatalf("history after failure = %+v, error = %v", messages, err)
			}
		})
	}
}

func newCompactor(t *testing.T, model kit.Model) *compaction.Compactor {
	t.Helper()

	compactor, err := compaction.NewCompactor(compaction.Config{}, staticModelResolver{model})
	if err != nil {
		t.Fatal(err)
	}

	return compactor
}

func receiveEvent(t *testing.T, subscription event.Subscription) event.Event {
	t.Helper()

	select {
	case received, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return received
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")

		return event.Event{}
	}
}

func messageEntry(t *testing.T, message kit.Message) entry.Entry {
	t.Helper()

	return encodedEntry(t, MessageEntryType, message)
}

func compactionEntry(t *testing.T, objective string, retained ...kit.Message) entry.Entry {
	t.Helper()

	return encodedEntry(t, compaction.EntryType, compaction.Compaction{
		Summary: compaction.Summary{Objective: objective}, Retained: retained,
	})
}

func encodedEntry(t *testing.T, kind string, value any) entry.Entry {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return entry.Entry{Type: kind, Payload: payload}
}
