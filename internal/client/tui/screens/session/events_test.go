package session

import (
	"context"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestUsageRebuildsFromJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	measured := func(text string, usage kit.Usage) kit.Message {
		message := kit.NewModelMessage(kit.NewTextContent(text))
		message.Usage = usage

		return message
	}

	records := []struct {
		executionID string
		entryType   string
		payload     any
	}{
		{"first", execution.StartedEntryType, execution.Started{Executor: agent.ExecutorName}},
		{"first", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("question"))},
		{"first", compaction.EntryType, compaction.Compaction{Usage: kit.Usage{InputTokens: 50, OutputTokens: 10}}},
		{"first", agent.MessageEntryType, measured("answer", kit.Usage{InputTokens: 1200, OutputTokens: 200, CacheReadTokens: 100})},
		{"first", execution.FinishedEntryType, execution.Finished{Status: execution.Completed}},
		{"manual", execution.StartedEntryType, execution.Started{Executor: compactionexec.ExecutorName}},
		{"manual", compaction.EntryType, compaction.Compaction{Usage: kit.Usage{InputTokens: 300, OutputTokens: 40}}},
		{"manual", execution.FinishedEntryType, execution.Finished{Status: execution.Completed}},
		{"second", execution.StartedEntryType, execution.Started{Executor: agent.ExecutorName}},
		{"second", agent.MessageEntryType, measured("reply", kit.Usage{InputTokens: 400, OutputTokens: 60})},
		{"second", execution.FinishedEntryType, execution.Finished{Status: execution.Completed}},
	}
	for _, record := range records {
		if err := opened.Commit(ctx, record.executionID, record.entryType, record.payload); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		screen := New(ctx, assistant.New(h, storage, nil, token.NewCharacterEstimator()), config.Config{}, opened.ID())
		_, next := screen.Update(screen.subscribe()())

		for range records {
			if next == nil {
				t.Fatal("journal replay stopped before all entries were received")
			}

			_, next = screen.Update(next())
		}

		screen.Close()

		if want := (kit.Usage{InputTokens: 1950, OutputTokens: 310, CacheReadTokens: 100}); screen.sessionUsage != want {
			t.Fatalf("session usage = %+v, want %+v", screen.sessionUsage, want)
		}
	}
}
