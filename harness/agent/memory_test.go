package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestAgentMemoryRecordsExecutionAttribution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := entry.NewMemoryStore()
	memory := NewMemory("session", "execution", store, event.NewBus("session", store))
	if err := memory.Record(ctx, kit.NewUserMessage(kit.NewTextContent("hello")), kit.NewModelMessage(kit.NewTextContent("hi"))); err != nil {
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
	original, err := memory.History(ctx)
	if err != nil || len(original) != 2 {
		t.Fatalf("History()=%+v, err=%v", original, err)
	}
}

func TestProjectHistory(t *testing.T) {
	t.Parallel()
	first := kit.NewUserMessage(kit.NewTextContent("first"))
	retained := kit.NewModelMessage(kit.NewTextContent("retained"))
	summary := compaction.Compaction{Summary: compaction.Summary{Objective: "summary"}}.Message()
	newSummary := compaction.Compaction{Summary: compaction.Summary{Objective: "new summary"}}.Message()
	later := kit.NewUserMessage(kit.NewTextContent("later"))
	tests := []struct {
		name    string
		entries []entry.Entry
		want    []kit.Message
	}{
		{"ordinary messages", []entry.Entry{messageEntry(t, first), messageEntry(t, retained)}, []kit.Message{first, retained}},
		{"summary and retained tail", []entry.Entry{messageEntry(t, first), messageEntry(t, retained), compactionEntry(t, "summary", retained), messageEntry(t, later)}, []kit.Message{summary, retained, later}},
		{"latest compaction", []entry.Entry{messageEntry(t, first), compactionEntry(t, "summary"), messageEntry(t, later), compactionEntry(t, "new summary", later)}, []kit.Message{newSummary, later}},
		{"no retained messages", []entry.Entry{messageEntry(t, first), compactionEntry(t, "summary"), messageEntry(t, later)}, []kit.Message{summary, later}},
		{"metadata has no effect", []entry.Entry{messageEntry(t, first), {Type: "execution.finished", Payload: json.RawMessage(`{}`)}}, []kit.Message{first}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := projectMessages(test.entries)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("projectMessages() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestContextMatchesReplayAndPreservesTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := entry.NewMemoryStore()
	first := kit.NewUserMessage(kit.NewTextContent("first"))
	retained := kit.NewModelMessage(kit.NewTextContent("retained"))
	later := kit.NewUserMessage(kit.NewTextContent("later"))
	_, err := store.Append(ctx, "session", messageEntry(t, first), messageEntry(t, retained), compactionEntry(t, "summary", retained), messageEntry(t, later), compactionEntry(t, "updated summary", later))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.List(ctx, "session", 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := projectMessages(stored)
	if err != nil {
		t.Fatal(err)
	}
	memory := NewMemory("session", "execution", store, event.NewBus("session", store))
	got, err := memory.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed context = %#v, replay = %#v", got, want)
	}
	original, err := memory.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, []kit.Message{first, retained, later}) {
		t.Fatalf("original history changed: %#v", original)
	}
	got[0].Content[0].Text.Text = "modified"
	again, err := memory.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatal("modifying returned messages affected subsequent reads")
	}
}

func TestProjectionRejectsMalformedRecords(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{`{}`, `{"objective":" "}`, `{"objective":"summary","retained":{}}`, `bad json`} {
		_, err := projectMessages([]entry.Entry{{ID: 42, Type: compaction.EntryType, Payload: json.RawMessage(payload)}})
		if err == nil || !strings.Contains(err.Error(), "42") {
			t.Fatalf("payload %s: error = %v", payload, err)
		}
	}
}

func TestMemoryReconstructsCompactedContextAfterReopen(t *testing.T) {
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
	memory := NewMemory("session", "next", store, event.NewBus("session", store))
	got, err := memory.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []kit.Message{compaction.Compaction{Summary: compaction.Summary{Objective: "summary"}}.Message(), retained, later}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened context = %#v, want %#v", got, want)
	}
	history, err := memory.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(history, []kit.Message{first, retained, later}) {
		t.Fatalf("reopened original transcript = %#v", history)
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
