package report

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/internal/service"
)

func TestContextShowsTokenBreakdown(t *testing.T) {
	report := service.ContextReport{
		Instructions: []service.ContextInstruction{
			{Tokens: 10},
			{Source: "Environment", Tokens: 20},
		},
		Messages: []service.ContextMessage{{Tokens: 30}},
		Tools: []service.ContextTool{
			{ToolDefinition: kit.ToolDefinition{Name: "read"}, Tokens: 5},
			{ToolDefinition: kit.ToolDefinition{Name: "bash"}, Tokens: 40},
		},
	}

	output := ansi.Strip(Context(report))

	text := strings.Join(strings.Fields(output), " ")
	for _, want := range []string{
		"105 tokens", "Conversation 30 1 message", "Tools 45 2 tools", "Instructions 30 2 sources",
		"Core 10", "Environment 20", "bash 40", "read 5",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("context output omitted %q: %s", want, output)
		}
	}

	sections := strings.Split(output, "\n\n")
	if len(sections) != 4 || !strings.HasPrefix(sections[1], "Conversation") || !strings.Contains(sections[1], "Instructions") {
		t.Fatalf("category overview did not precede details: %s", output)
	}

	if strings.Index(output, "  Core") >= strings.Index(output, "  Environment") {
		t.Fatalf("instruction sources changed order: %s", output)
	}

	if strings.Index(output, "  bash") >= strings.Index(output, "  read") || report.Tools[0].Name != "read" {
		t.Fatalf("tool costs were not sorted without changing the report: %s", output)
	}

	if strings.Contains(output, "≈") {
		t.Fatalf("context output contains approximation markers: %s", output)
	}
}

func TestContextGroupsMCPToolsByServer(t *testing.T) {
	report := service.ContextReport{
		Tools: []service.ContextTool{
			{ToolDefinition: kit.ToolDefinition{Name: "read"}, Tokens: 5},
			{ToolDefinition: kit.ToolDefinition{Name: "mcp__github__search"}, Tokens: 50},
			{ToolDefinition: kit.ToolDefinition{Name: "mcp__linear__list_issues"}, Tokens: 900},
			{ToolDefinition: kit.ToolDefinition{Name: "mcp__linear__get_issue"}, Tokens: 400},
		},
	}

	output := ansi.Strip(Context(report))

	text := strings.Join(strings.Fields(output), " ")
	for _, want := range []string{"Tools 1.4k 4 tools", "MCP linear 1.3k 2 tools", "MCP github 50 1 tool", "read 5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("context output omitted %q: %s", want, output)
		}
	}

	if strings.Contains(output, "list_issues") || strings.Index(output, "MCP linear") >= strings.Index(output, "MCP github") {
		t.Fatalf("MCP tools were not grouped by server and sorted by cost: %s", output)
	}
}

func TestContextStripsTerminalEscapesFromLabels(t *testing.T) {
	report := service.ContextReport{
		Instructions: []service.ContextInstruction{{Source: "before\x1b[2Jafter", Tokens: 10}},
		Tools:        []service.ContextTool{{ToolDefinition: kit.ToolDefinition{Name: "tool\x1b[2Jname"}, Tokens: 20}},
	}

	output := Context(report)
	if strings.Contains(output, "\x1b[2J") || !strings.Contains(ansi.Strip(output), "beforeafter") || !strings.Contains(ansi.Strip(output), "toolname") {
		t.Fatalf("unsafe context labels: %q", output)
	}
}
