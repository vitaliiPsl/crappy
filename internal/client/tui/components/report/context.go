package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/service"
)

func Context(report service.ContextReport) string {
	labelWidth := len("Instructions")

	tokenWidth := max(
		len(Tokens(int64(report.Tokens()))),
		len(Tokens(int64(report.MessageTokens()))),
		len(Tokens(int64(report.ToolTokens()))),
		len(Tokens(int64(report.InstructionTokens()))),
	)
	for _, instruction := range report.Instructions {
		labelWidth = max(labelWidth, 2+ansi.StringWidth(ansi.Strip(cmp.Or(instruction.Source, "Core"))))
		tokenWidth = max(tokenWidth, len(Tokens(int64(instruction.Tokens))))
	}

	tools := groupTools(report.Tools)
	for _, tool := range tools {
		labelWidth = max(labelWidth, 2+ansi.StringWidth(ansi.Strip(tool.label)))
		tokenWidth = max(tokenWidth, len(Tokens(int64(tool.tokens))))
	}

	row := func(label string, tokens int, count string, style lipgloss.Style) string {
		label = ansi.Strip(label)
		padding := strings.Repeat(" ", max(labelWidth-ansi.StringWidth(label), 0))
		value := fmt.Sprintf("%*s", tokenWidth, Tokens(int64(tokens)))

		line := style.Render(label+padding) + "  " + valueStyle.Render(value)
		if count != "" {
			line += "  " + countStyle.Render(count)
		}

		return line
	}

	lines := []string{
		headingStyle.Render(Tokens(int64(report.Tokens()))) + " " + countStyle.Render("tokens"),
		countStyle.Render("Tools reflect the initial selection; running work may load more."),
		"",
		row("Conversation", report.MessageTokens(), Count(len(report.Messages), "message"), valueStyle),
		row("Tools", report.ToolTokens(), Count(len(report.Tools), "tool"), valueStyle),
		row("Instructions", report.InstructionTokens(), Count(len(report.Instructions), "source"), valueStyle),
	}
	if len(report.Instructions) > 0 {
		lines = append(lines, "", headingStyle.Render("Instruction sources"))
		for _, instruction := range report.Instructions {
			lines = append(lines, row("  "+cmp.Or(instruction.Source, "Core"), instruction.Tokens, "", detailStyle))
		}
	}

	if len(tools) > 0 {
		lines = append(lines, "", headingStyle.Render("Tool definitions"))
		for _, tool := range tools {
			count := ""
			if tool.count > 0 {
				count = Count(tool.count, "tool")
			}

			lines = append(lines, row("  "+tool.label, tool.tokens, count, detailStyle))
		}
	}

	return strings.Join(lines, "\n")
}

type toolGroup struct {
	label  string
	tokens int
	count  int
}

func groupTools(tools []service.ContextTool) []toolGroup {
	var groups []toolGroup

	servers := make(map[string]int)

	for _, tool := range tools {
		server, ok := mcpServer(tool.Name)
		if !ok {
			groups = append(groups, toolGroup{label: tool.Name, tokens: tool.Tokens})

			continue
		}

		index, seen := servers[server]
		if !seen {
			index = len(groups)
			servers[server] = index
			groups = append(groups, toolGroup{label: "MCP " + server})
		}

		groups[index].tokens += tool.Tokens
		groups[index].count++
	}

	slices.SortStableFunc(groups, func(a, b toolGroup) int {
		return cmp.Compare(b.tokens, a.tokens)
	})

	return groups
}

func mcpServer(tool string) (string, bool) {
	name, found := strings.CutPrefix(tool, "mcp__")
	if !found {
		return "", false
	}

	server, _, found := strings.Cut(name, "__")

	return server, found
}
