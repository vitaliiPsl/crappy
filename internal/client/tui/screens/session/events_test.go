package session

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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

	opened, err := h.CreateSession(ctx, nil)
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
		{"first", execution.EntryType, execution.Record{Status: execution.Running, Executor: agent.ExecutorName}},
		{"first", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("question"))},
		{"first", compaction.EntryType, compaction.Compaction{Usage: kit.Usage{InputTokens: 50, OutputTokens: 10}}},
		{"first", agent.MessageEntryType, measured("answer", kit.Usage{InputTokens: 1200, OutputTokens: 200, CacheReadTokens: 100})},
		{"first", execution.EntryType, execution.Record{Status: execution.Completed}},
		{"manual", execution.EntryType, execution.Record{Status: execution.Running, Executor: compactionexec.ExecutorName}},
		{"manual", compaction.EntryType, compaction.Compaction{Usage: kit.Usage{InputTokens: 300, OutputTokens: 40}}},
		{"manual", execution.EntryType, execution.Record{Status: execution.Completed}},
		{"second", execution.EntryType, execution.Record{Status: execution.Running, Executor: agent.ExecutorName}},
		{"second", agent.MessageEntryType, measured("reply", kit.Usage{InputTokens: 400, OutputTokens: 60})},
		{"second", execution.EntryType, execution.Record{Status: execution.Completed}},
	}
	for _, record := range records {
		if err := opened.Commit(ctx, record.executionID, record.entryType, record.payload); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		screen := New(ctx, assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator()), opened.ID())
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

func TestJournalReplayPreservesCompletedResumedResponse(t *testing.T) {
	call := kit.NewToolCall("call-1", "bash", map[string]any{"command": "go test ./..."})
	records := []struct {
		id      string
		kind    string
		payload any
	}{
		{"analysis", execution.EntryType, execution.Record{Status: execution.Queued}},
		{"analysis", execution.EntryType, execution.Record{Status: execution.Running}},
		{"analysis", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("Analyze repository"))},
		{"analysis", agent.MessageEntryType, kit.NewModelMessage(kit.NewToolCallContent(call))},
		{"analysis", agent.MessageEntryType, kit.NewToolMessage(kit.NewToolResultContent(kit.NewToolResult(call, kit.ToolOutput{}, nil)))},
		{"review", execution.EntryType, execution.Record{Status: execution.Queued}},
		{"analysis", execution.EntryType, execution.Record{Status: execution.Interrupted}},
		{"analysis", execution.EntryType, execution.Record{Status: execution.Running}},
		{"analysis", agent.MessageEntryType, kit.NewModelMessage(kit.NewTextContent("Repository analysis response"))},
		{"analysis", execution.EntryType, execution.Record{Status: execution.Completed}},
		{"review", execution.EntryType, execution.Record{Status: execution.Running}},
		{"review", agent.MessageEntryType, kit.NewUserMessage(kit.NewTextContent("Review core instructions"))},
		{"review", agent.MessageEntryType, kit.NewModelMessage(kit.NewTextContent("Instructions review response"))},
		{"review", execution.EntryType, execution.Record{Status: execution.Completed}},
	}

	for range 2 {
		screen := New(context.Background(), nil, "session")
		screen.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

		for _, record := range records {
			screen.applyEvent(executionEvent(t, record.kind, record.id, record.payload))
			_ = screen.View()
		}

		view := screen.View().Content
		tool := strings.Index(view, "go test ./...")
		response := strings.Index(view, "Repository analysis response")
		next := strings.Index(view, "Review core instructions")

		if tool < 0 || response <= tool || next <= response || strings.Count(view, "Repository analysis response") != 1 {
			t.Fatalf("reopening lost or reordered the resumed response: %q", view)
		}

		screen.Close()
	}
}
