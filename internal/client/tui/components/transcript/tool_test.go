package transcript

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/vitaliipsl/crappy/internal/prompt"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestQuestionUsesStandardToolRendering(t *testing.T) {
	call := kit.NewToolCall("question", prompt.ToolName, map[string]any{"question": "Which deadline?"})
	success := kit.NewToolResult(call, kit.NewStructuredToolOutput("Friday"), nil)
	failure := kit.NewToolResult(call, kit.NewStructuredToolOutput(""), errors.New("interrupted"))

	for _, result := range []*kit.ToolResult{nil, &success, &failure} {
		question := renderTool(toolUse{call: call, result: result}, 80)
		plain := ansi.Strip(question)

		if !strings.Contains(plain, "ask_user  Which deadline?") || !strings.Contains(plain, "│") {
			t.Fatalf("question is not displayed as a tool: %q", plain)
		}

		ordinary := call
		ordinary.Name = "run_tool"
		ordinary.Arguments = map[string]any{"text": "Which deadline?"}
		want := renderTool(toolUse{call: ordinary, result: result}, 80)

		if strings.ReplaceAll(question, prompt.ToolName, ordinary.Name) != want {
			t.Fatalf("question rendering differs from ordinary tools: %q", question)
		}
	}
}
