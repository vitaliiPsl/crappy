package session

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	errorPrefix        = "Error: "
	workingLabel       = "Working… Esc to interrupt"
	interruptedLabel   = "Interrupted"
	sectionSeparator   = "\n"
	minimumContentSize = 1
)

func (m *Model) render() string {
	status := m.statusView()

	m.composer.SetWidth(m.contentWidth())
	m.transcript.SetSize(m.contentWidth(), m.transcriptHeight(status))

	parts := []string{transcriptAreaStyle.Render(m.transcript.View())}
	if status != "" {
		parts = append(parts, status)
	}

	parts = append(parts, m.composer.View())

	return pageStyle.Render(strings.Join(parts, sectionSeparator))
}

func (m *Model) transcriptHeight(status string) int {
	height := m.height -
		pageStyle.GetVerticalFrameSize() -
		transcriptAreaStyle.GetVerticalFrameSize() -
		m.composer.Height()

	if status != "" {
		height -= lipgloss.Height(status)
	}

	return max(height, minimumContentSize)
}

func (m *Model) statusView() string {
	if m.running() {
		return workingStyle.Width(m.contentWidth()).Render(workingLabel)
	}

	switch m.notice.kind {
	case noticeError:
		return errorStyle.Width(m.contentWidth()).Render(errorPrefix + m.notice.message)
	case noticeInterrupted:
		return noticeStyle.Width(m.contentWidth()).Render(interruptedLabel)
	case noticeNone:
	}

	return ""
}

func (m *Model) contentWidth() int {
	return max(m.width-pageStyle.GetHorizontalFrameSize(), minimumContentSize)
}
