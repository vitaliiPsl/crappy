package transcript

import (
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestDurableMessageReplacesStream(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Apply(kit.NewAgentContentDeltaEvent(kit.NewTextContent("partial")))
	model.Append(kit.NewModelMessage(kit.NewTextContent("complete")))

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
	model.Append(kit.NewModelMessage(kit.NewToolCallContent(call)))
	model.Append(kit.NewToolMessage(kit.NewToolResultContent(result)))

	view := model.View()
	if strings.Count(view, "bash") != 1 || !strings.Contains(view, "go test ./...") {
		t.Fatalf("view = %q, want paired tool call and result", view)
	}
}
