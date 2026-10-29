package session

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

const (
	errorPrefix        = "Error: "
	workingLabel       = "Working…"
	interruptedLabel   = "Interrupted"
	sectionSeparator   = "\n"
	minimumContentSize = 1
)

func (m *Model) render() string {
	header := m.headerView()
	status := m.statusView()
	shortcuts := m.shortcutsView()

	m.composer.SetWidth(m.contentWidth())
	m.transcript.SetSize(m.contentWidth(), m.transcriptHeight(header, status, shortcuts))

	parts := []string{header, transcriptAreaStyle.Render(m.transcript.View())}
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
	modelID := cmp.Or(m.config.ModelID, "No model selected")

	model := modelID
	if m.config.ProviderID != "" {
		model = m.config.ProviderID + " / " + model
	}

	details := model + " · " + cmp.Or(m.config.Generation.Thinking, "default")
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
	var values []hints.Hint

	if m.terminalErr == nil {
		if !m.running() {
			values = append(values, hints.Hint{Key: "Enter", Label: "Send"})
		}

		values = append(values, hints.Hint{Key: "Shift+Enter", Label: "Newline"})
	}

	values = append(values,
		hints.Hint{Key: "Ctrl+C", Label: "Quit"},
	)

	if m.running() {
		values = append(values, hints.Hint{Key: "Esc", Label: "Interrupt"})
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

	if m.notice.kind == noticeInfo {
		return infoStyle.Width(m.contentWidth()).Render(m.notice.message)
	}

	if m.running() {
		return workingStyle.Width(m.contentWidth()).Render(workingLabel)
	}

	if m.notice.kind == noticeInterrupted {
		return noticeStyle.Width(m.contentWidth()).Render(interruptedLabel)
	}

	return ""
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
