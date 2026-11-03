package assistant

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vitaliipsl/crappy/internal/config"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestUsageReadsPersistedFactsWithoutChangingHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	opened, err := h.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	message := kit.NewModelMessage(kit.NewTextContent("done"))

	message.Usage = kit.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 15}
	if err := opened.Commit(ctx, "agent", agent.MessageEntryType, message); err != nil {
		t.Fatal(err)
	}

	if got, err := app.Usage(ctx, opened.ID()); err != nil || got != message.Usage {
		t.Fatalf("first query = %+v, %v", got, err)
	}

	compacted := compaction.Compaction{Retained: []kit.Message{message}, Usage: kit.Usage{InputTokens: 50, OutputTokens: 10}}
	if err := opened.Commit(ctx, "compact", compaction.EntryType, compacted); err != nil {
		t.Fatal(err)
	}

	before, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		usage, err := app.Usage(ctx, opened.ID())
		if want := (kit.Usage{InputTokens: 150, OutputTokens: 30, CacheReadTokens: 15}); err != nil || usage != want {
			t.Fatalf("usage = %+v, error = %v; want %+v", usage, err, want)
		}
	}

	after, err := opened.Entries(ctx, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("usage query changed history: %+v, %v", after, err)
	}
}

func TestUsageFromEventRejectsMalformedUsageAndIgnoresOtherRecords(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{agent.MessageEntryType, compaction.EntryType} {
		if _, err := UsageFromEvent(event.Event{Type: kind, Payload: json.RawMessage(`invalid`)}); err == nil {
			t.Fatalf("malformed %s was accepted", kind)
		}
	}

	if usage, err := UsageFromEvent(event.Event{Type: "extension.data", Payload: json.RawMessage(`invalid`)}); err != nil || usage != (kit.Usage{}) {
		t.Fatalf("unrelated record = %+v, %v", usage, err)
	}
}
