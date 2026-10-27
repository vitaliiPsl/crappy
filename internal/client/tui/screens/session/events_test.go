package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
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

	manualUsage := kit.Usage{InputTokens: 300, OutputTokens: 40}

	manualOutput, err := json.Marshal(compactionexec.Result{Compacted: true, Usage: manualUsage})
	if err != nil {
		t.Fatal(err)
	}

	records := []struct {
		executionID string
		entryType   string
		payload     any
	}{
		{"first", execution.StartedEntryType, execution.Started{Executor: agent.ExecutorName}},
		{"first", compaction.EntryType, compaction.Compaction{Usage: kit.Usage{InputTokens: 50, OutputTokens: 10}}},
		{"first", agent.FinishedEntryType, agent.Finished{
			Usage:     kit.Usage{InputTokens: 1200, OutputTokens: 200, CacheReadTokens: 100},
			LastUsage: kit.Usage{InputTokens: 100, OutputTokens: 20},
		}},
		{"first", execution.FinishedEntryType, execution.Result{Outcome: execution.Completed}},
		{"manual", execution.StartedEntryType, execution.Started{Executor: compactionexec.ExecutorName}},
		{"manual", compaction.EntryType, compaction.Compaction{Usage: manualUsage}},
		{"manual", execution.FinishedEntryType, execution.Result{Outcome: execution.Completed, Output: manualOutput}},
		{"second", execution.StartedEntryType, execution.Started{Executor: agent.ExecutorName}},
		{"second", agent.FinishedEntryType, agent.Finished{Usage: kit.Usage{InputTokens: 400, OutputTokens: 60}}},
		{"second", execution.FinishedEntryType, execution.Result{Outcome: execution.Completed}},
	}
	for _, record := range records {
		if err := opened.Commit(ctx, record.executionID, record.entryType, record.payload); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		screen := New(ctx, h, config.Config{}, opened.ID())
		_, next := screen.Update(screen.subscribe()())

		for range records {
			if next == nil {
				t.Fatal("journal replay stopped before all entries were received")
			}

			_, next = screen.Update(next())
		}

		screen.Close()

		if want := (kit.Usage{InputTokens: 1900, OutputTokens: 300, CacheReadTokens: 100}); screen.sessionUsage != want {
			t.Fatalf("session usage = %+v, want %+v", screen.sessionUsage, want)
		}
	}
}
