package session

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/report"
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
	m.composer.SetWidth(m.contentWidth())
	m.prompt.SetSize(m.contentWidth(), max(5, m.height/2))
	m.queue.SetSize(m.contentWidth(), m.height)

	header := m.headerView()
	status := m.statusView()
	shortcuts := m.shortcutsView()

	available := m.height - pageStyle.GetVerticalFrameSize() - transcriptAreaStyle.GetVerticalFrameSize() -
		lipgloss.Height(header) - lipgloss.Height(status) - lipgloss.Height(shortcuts) - minimumContentSize
	m.prompt.SetSize(m.contentWidth(), min(max(5, m.height/2), max(5, available)))

	height := m.transcriptHeight(header, status, shortcuts)
	m.transcript.SetSize(m.contentWidth(), height)
	m.queue.SetSize(m.contentWidth(), height)

	body := m.transcript.View()
	if m.queue.Opened() {
		body = m.queue.View()
	}

	parts := []string{header, transcriptAreaStyle.Render(body)}
	if status != "" {
		parts = append(parts, status)
	}

	parts = append(parts, m.inputView(), shortcuts)

	return pageStyle.Render(strings.Join(parts, sectionSeparator))
}

func (m *Model) transcriptHeight(header, status, shortcuts string) int {
	height := m.height -
		pageStyle.GetVerticalFrameSize() -
		transcriptAreaStyle.GetVerticalFrameSize() -
		lipgloss.Height(m.inputView()) -
		lipgloss.Height(header) -
		lipgloss.Height(shortcuts)

	if status != "" {
		height -= lipgloss.Height(status)
	}

	return max(height, minimumContentSize)
}

func (m *Model) inputView() string {
	if m.prompt.Active() {
		return m.prompt.View()
	}

	return m.composer.View()
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
	return fmt.Sprintf("%s in / %s out", report.Tokens(m.sessionUsage.InputTokens), report.Tokens(m.sessionUsage.OutputTokens))
}

func (m *Model) shortcutsView() string {
	if m.queue.Opened() {
		return m.queue.Shortcuts(m.terminalErr == nil)
	}

	if m.prompt.Active() {
		return m.prompt.Shortcuts()
	}

	var values []hints.Hint

	if m.terminalErr == nil {
		label := "Send"
		if len(m.pending) > 0 {
			label = "Queue"
		}

		values = append(values, hints.Hint{Key: "Enter", Label: label})
		if m.running() {
			values = append(values, hints.Hint{Key: "Alt+Enter", Label: "Steer"})
		}

		values = append(values, hints.Hint{Key: "Shift+Enter", Label: "Newline"})
	}

	values = append(values,
		hints.Hint{Key: "Ctrl+C", Label: "Close"},
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
	if m.prompt.Active() {
		return ""
	} else if m.running() {
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
