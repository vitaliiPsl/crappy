package session

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

const (
	errorPrefix        = "Error: "
	workingLabel       = "Working…"
	interruptedLabel   = "Interrupted"
	cancelledLabel     = "Cancelled"
	sectionSeparator   = "\n"
	minimumContentSize = 1
)

func (m *Model) render() string {
	header := m.headerView()
	status := m.statusView()
	shortcuts := m.shortcutsView()

	m.composer.SetWidth(m.contentWidth())
	height := m.transcriptHeight(header, status, shortcuts)
	m.transcript.SetSize(m.contentWidth(), height)

	body := m.transcript.View()
	if m.queueOpen {
		body = m.queueView(height)
	}

	parts := []string{header, transcriptAreaStyle.Render(body)}
	if status != "" {
		parts = append(parts, status)
	}

	parts = append(parts, m.composer.View(), shortcuts)

	return pageStyle.Render(strings.Join(parts, sectionSeparator))
}

func (m *Model) transcriptHeight(header, status, shortcuts string) int {
	height := m.height -
		pageStyle.GetVerticalFrameSize() -
		transcriptAreaStyle.GetVerticalFrameSize() -
		m.composer.Height() -
		lipgloss.Height(header) -
		lipgloss.Height(shortcuts)

	if status != "" {
		height -= lipgloss.Height(status)
	}

	return max(height, minimumContentSize)
}

func (m *Model) headerView() string {
	width := m.contentWidth()
	usage := ansi.Truncate(m.usageLabel(), width, "…")
	available := max(0, width-ansi.StringWidth(usage)-2)
	modelID := cmp.Or(m.model.ModelID, "No model selected")

	model := modelID
	if m.model.ProviderID != "" {
		model = m.model.ProviderID + " / " + model
	}

	details := model + " · " + cmp.Or(m.model.Thinking, "default")
	if m.workdir != "" && available > 3 {
		directory := m.workdir
		directoryWidth := min(ansi.StringWidth(directory), available/2)

		modelWidth := max(0, available-directoryWidth-3)
		if ansi.StringWidth(details) > modelWidth {
			details = model
		}

		if ansi.StringWidth(details) > modelWidth {
			details = modelID
		}

		if overflow := ansi.StringWidth(directory) - directoryWidth; overflow > 0 {
			directory = "…" + ansi.TruncateLeft(directory, overflow+1, "")
		}

		details = directory + " · " + ansi.Truncate(details, modelWidth, "…")
	}

	details = ansi.Truncate(details, available, "…")
	gap := width - ansi.StringWidth(details) - ansi.StringWidth(usage)

	return headerStyle.Width(width).Render(details + strings.Repeat(" ", gap) + usage)
}

func (m *Model) usageLabel() string {
	return fmt.Sprintf("%s in / %s out", formatTokens(m.sessionUsage.InputTokens), formatTokens(m.sessionUsage.OutputTokens))
}

func (m *Model) shortcutsView() string {
	if m.queueOpen {
		return m.queueHints()
	}

	var values []hints.Hint

	if m.terminalErr == nil {
		label := "Send"
		if len(m.pending) > 0 {
			label = "Queue"
		}

		values = append(values, hints.Hint{Key: "Enter", Label: label})
		values = append(values, hints.Hint{Key: "Shift+Enter", Label: "Newline"})
	}

	values = append(values,
		hints.Hint{Key: "Ctrl+C", Label: "Quit"},
	)

	if m.running() {
		values = append(values, hints.Hint{Key: "Esc", Label: "Cancel current"})
	}

	return hints.Render(m.contentWidth(), values...)
}

func (m *Model) statusView() string {
	if m.terminalErr != nil {
		return errorStyle.Width(m.contentWidth()).Render(errorPrefix + m.terminalErr.Error())
	}

	if m.notice.kind == noticeError {
		return errorStyle.Width(m.contentWidth()).Render(errorPrefix + m.notice.message)
	}

	label := ""
	style := workingStyle

	queued := m.queuedCount()
	if m.running() {
		label = workingLabel
		if queued > 0 {
			label += fmt.Sprintf(" · %d queued", queued)
		}
	} else if len(m.pending) > 0 {
		style = noticeStyle

		if m.pending[0].Status == execution.Interrupted {
			label = interruptedLabel
			if queued > 0 {
				label += fmt.Sprintf(" · %d queued", queued)
			}

			label += " · /resume to continue"
		} else {
			label = fmt.Sprintf("%d queued · /resume to start", queued)
		}
	}

	if label == "" && m.notice.kind == noticeCancelled {
		label, style = cancelledLabel, noticeStyle
	}

	if label == "" {
		return ""
	}

	return style.Width(m.contentWidth()).Render(ansi.Truncate(label, m.contentWidth(), "…"))
}

func (m *Model) contentWidth() int {
	return max(m.width-pageStyle.GetHorizontalFrameSize(), minimumContentSize)
}

func formatTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func renderContext(report assistant.ContextReport) string {
	labelWidth := len("Instructions")

	tokenWidth := max(
		len(formatTokens(int64(report.Tokens()))),
		len(formatTokens(int64(report.MessageTokens()))),
		len(formatTokens(int64(report.ToolTokens()))),
		len(formatTokens(int64(report.InstructionTokens()))),
	)
	for _, instruction := range report.Instructions {
		labelWidth = max(labelWidth, 2+ansi.StringWidth(ansi.Strip(cmp.Or(instruction.Source, "Core"))))
		tokenWidth = max(tokenWidth, len(formatTokens(int64(instruction.Tokens))))
	}

	for _, tool := range report.Tools {
		labelWidth = max(labelWidth, 2+ansi.StringWidth(ansi.Strip(tool.Name)))
		tokenWidth = max(tokenWidth, len(formatTokens(int64(tool.Tokens))))
	}

	row := func(label string, tokens int, count string, style lipgloss.Style) string {
		label = ansi.Strip(label)
		padding := strings.Repeat(" ", max(labelWidth-ansi.StringWidth(label), 0))
		value := fmt.Sprintf("%*s", tokenWidth, formatTokens(int64(tokens)))

		line := style.Render(label+padding) + "  " + contextValueStyle.Render(value)
		if count != "" {
			line += "  " + contextCountStyle.Render(count)
		}

		return line
	}

	lines := []string{
		contextHeadingStyle.Render(formatTokens(int64(report.Tokens()))) + " " + contextCountStyle.Render("tokens"),
		"",
		row("Conversation", report.MessageTokens(), countLabel(len(report.Messages), "message"), contextValueStyle),
		row("Tools", report.ToolTokens(), countLabel(len(report.Tools), "tool"), contextValueStyle),
		row("Instructions", report.InstructionTokens(), countLabel(len(report.Instructions), "source"), contextValueStyle),
	}
	if len(report.Instructions) > 0 {
		lines = append(lines, "", contextHeadingStyle.Render("Instruction sources"))
		for _, instruction := range report.Instructions {
			lines = append(lines, row("  "+cmp.Or(instruction.Source, "Core"), instruction.Tokens, "", contextDetailStyle))
		}
	}

	if len(report.Tools) > 0 {
		tools := slices.Clone(report.Tools)
		slices.SortStableFunc(tools, func(a, b assistant.ContextTool) int {
			return cmp.Compare(b.Tokens, a.Tokens)
		})

		lines = append(lines, "", contextHeadingStyle.Render("Tool definitions"))
		for _, tool := range tools {
			lines = append(lines, row("  "+tool.Name, tool.Tokens, "", contextDetailStyle))
		}
	}

	return strings.Join(lines, "\n")
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}
