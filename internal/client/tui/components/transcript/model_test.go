package transcript

import (
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/compaction"
)

func TestDurableMessageReplacesStream(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Apply("execution-1", kit.NewAgentContentDeltaEvent(kit.NewTextContent("partial")))
	model.Append("execution-1", kit.NewModelMessage(kit.NewTextContent("complete")))

	view := model.View()
	if !strings.Contains(view, "complete") || strings.Contains(view, "partial") {
		t.Fatalf("view = %q, want only durable response", view)
	}
}

func TestToolCallAndResultRenderTogether(t *testing.T) {
	call := kit.NewToolCall("call-1", "bash", map[string]any{"command": "go test ./..."})
	result := kit.NewToolResult(call, kit.ToolOutput{}, nil)
	model := New()
	model.SetSize(80, 20)
	model.Append("execution-1", kit.NewModelMessage(kit.NewToolCallContent(call)))
	model.Append("execution-1", kit.NewToolMessage(kit.NewToolResultContent(result)))

	view := model.View()
	if strings.Count(view, "bash") != 1 || !strings.Contains(view, "go test ./...") {
		t.Fatalf("view = %q, want paired tool call and result", view)
	}
}

func TestFinishedExecutionCachesItsRender(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Start("execution-1")
	model.Append("execution-1", kit.NewUserMessage(kit.NewTextContent("question")))
	model.Finish("execution-1")

	_ = model.View()

	finished := model.executions[0]
	if finished.width != 80 {
		t.Fatalf("cached width = %d, want 80", finished.width)
	}

	finished.rendered = "SENTINEL"

	if !strings.Contains(model.render(), "SENTINEL") {
		t.Fatal("render() did not reuse the finished execution's cache")
	}

	model.SetSize(90, 20)

	if strings.Contains(model.render(), "SENTINEL") {
		t.Fatal("render() reused a cached execution at a different width")
	}
}

func TestActiveExecutionIsNotCached(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Start("execution-1")
	model.Append("execution-1", kit.NewUserMessage(kit.NewTextContent("question")))

	_ = model.View()

	model.executions[0].rendered = "SENTINEL"

	if strings.Contains(model.render(), "SENTINEL") {
		t.Fatal("render() reused a cache for a running execution")
	}
}

func TestFinishDiscardsStreamedOutput(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Start("execution-1")
	model.Append("execution-1", kit.NewUserMessage(kit.NewTextContent("question")))
	model.Apply("execution-1", kit.NewAgentContentDeltaEvent(kit.NewTextContent("PARTIAL")))

	if !strings.Contains(model.View(), "PARTIAL") {
		t.Fatalf("view = %q, want streamed output while running", model.View())
	}

	model.Finish("execution-1")

	if strings.Contains(model.View(), "PARTIAL") {
		t.Fatalf("view = %q, want streamed output discarded", model.View())
	}

	if !strings.Contains(model.View(), "question") {
		t.Fatalf("view = %q, want recorded messages kept", model.View())
	}
}

func TestExecutionsAreSeparate(t *testing.T) {
	model := New()
	model.SetSize(80, 20)

	model.Start("execution-1")
	model.Append("execution-1", kit.NewUserMessage(kit.NewTextContent("first")))
	model.Finish("execution-1")

	model.Start("execution-2")
	model.Append("execution-2", kit.NewUserMessage(kit.NewTextContent("second")))

	if got, want := len(model.executions), 2; got != want {
		t.Fatalf("executions = %d, want %d", got, want)
	}

	view := model.View()
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatalf("view = %q, want both executions", view)
	}
}

func TestCompactionRendersPendingThenCompacted(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.StartCompaction("execution-1")

	if view := model.View(); !strings.Contains(view, compactionPendingLabel) {
		t.Fatalf("view = %q, want pending compaction", view)
	}

	model.AppendCompaction("execution-1", compaction.Compaction{
		Summary: compaction.Summary{Objective: "ship compaction"},
		Retained: []kit.Message{
			kit.NewUserMessage(kit.NewTextContent("one")),
			kit.NewUserMessage(kit.NewTextContent("two")),
			kit.NewUserMessage(kit.NewTextContent("three")),
		},
		TokensBefore: 120_400,
		TokensAfter:  850,
	})

	view := model.View()
	if strings.Contains(view, compactionPendingLabel) ||
		!strings.Contains(view, compactionLabel) ||
		!strings.Contains(view, "120.4k → 850 tokens") ||
		!strings.Contains(view, "kept 3 messages") {
		t.Fatalf("view = %q, want completed compaction", view)
	}
}

func TestFinishClearsPendingCompaction(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.StartCompaction("execution-1")
	model.Finish("execution-1")

	if view := model.View(); strings.Contains(view, compactionPendingLabel) {
		t.Fatalf("view = %q, want no pending compaction after finish", view)
	}
}
