package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/config"
)

func TestViewShowsAgentContextAndUsage(t *testing.T) {
	for _, thinking := range []string{"", "high"} {
		t.Run("reasoning="+thinking, func(t *testing.T) {
			screen := New(context.Background(), nil, config.Config{
				ProviderID: "openai",
				ModelID:    "test-model",
				Generation: config.Generation{Thinking: thinking},
			}, "session-1")
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
			screen.sessionUsage = kit.Usage{InputTokens: 3400, OutputTokens: 900}
			screen.workdir = "/projects/crappy"

			if thinking == "" {
				thinking = "default"
			}

			view := screen.View().Content
			for _, want := range []string{
				"/projects/crappy · openai / test-model · " + thinking,
				"3.4k in / 900 out",
				"Shift+Enter",
				"Ctrl+C",
			} {
				if !strings.Contains(view, want) {
					t.Fatalf("view is missing %q: %q", want, view)
				}
			}
		})
	}
}

func TestViewFitsWindowWithContext(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {80, 20}, {120, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			screen := New(context.Background(), nil, config.Config{
				ProviderID: "openai",
				ModelID:    "test-model",
				Generation: config.Generation{Thinking: "high"},
			}, "session-1")
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen.sessionUsage = kit.Usage{InputTokens: 1200, OutputTokens: 300}
			screen.workdir = "/projects/" + strings.Repeat("long-directory/", 12) + "crappy"
			screen.applyEvent(executionEvent(t, execution.StartedEntryType, "execution-1", execution.Started{}))

			if height := lipgloss.Height(screen.headerView()); height != 1+headerMarginBottom {
				t.Fatalf("header height = %d, want one line with its bottom margin", height)
			}

			view := screen.View().Content
			if width, height := lipgloss.Width(view), lipgloss.Height(view); width != size[0] || height != size[1] {
				t.Fatalf("view = %dx%d, want %dx%d: %q", width, height, size[0], size[1], view)
			}

			if !strings.Contains(view, "Esc") {
				t.Fatal("running execution has no interrupt hint")
			}

			if !strings.Contains(view, "…") || !strings.Contains(view, "crappy") {
				t.Fatalf("long workdir is missing its suffix: %q", view)
			}
		})
	}
}

func TestContextOutputShowsTokenBreakdown(t *testing.T) {
	report := assistant.ContextReport{
		Instructions: []assistant.ContextInstruction{
			{Tokens: 10},
			{Source: "Environment", Tokens: 20},
		},
		Messages: []assistant.ContextMessage{{Tokens: 30}},
		Tools: []assistant.ContextTool{
			{ToolDefinition: kit.ToolDefinition{Name: "read"}, Tokens: 5},
			{ToolDefinition: kit.ToolDefinition{Name: "bash"}, Tokens: 40},
		},
	}

	output := ansi.Strip(renderContext(report))

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

func TestContextOutputStripsTerminalEscapesFromLabels(t *testing.T) {
	report := assistant.ContextReport{
		Instructions: []assistant.ContextInstruction{{Source: "before\x1b[2Jafter", Tokens: 10}},
		Tools:        []assistant.ContextTool{{ToolDefinition: kit.ToolDefinition{Name: "tool\x1b[2Jname"}, Tokens: 20}},
	}

	output := renderContext(report)
	if strings.Contains(output, "\x1b[2J") || !strings.Contains(ansi.Strip(output), "beforeafter") || !strings.Contains(ansi.Strip(output), "toolname") {
		t.Fatalf("unsafe context labels: %q", output)
	}
}
