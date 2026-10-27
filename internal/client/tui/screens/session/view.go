package session

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

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
	model := cmp.Or(m.config.ModelID, "No model selected")
	if m.config.ProviderID != "" {
		model = m.config.ProviderID + " / " + model
	}

	details := model + " · " + cmp.Or(m.config.Generation.Thinking, "default") + " reasoning"
	usage := m.usageLabel()
	content := details + "\n" + usage

	gap := m.contentWidth() - lipgloss.Width(details) - lipgloss.Width(usage)
	if gap >= 2 {
		content = details + strings.Repeat(" ", gap) + usage
	}

	return headerStyle.Width(m.contentWidth()).Render(content)
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
		hints.Hint{Key: "Ctrl+P", Label: "Sessions"},
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
