package session

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	errorPrefix        = "Error: "
	sectionSeparator   = "\n"
	minimumContentSize = 1
)

func (m *Model) render() string {
	parts := []string{transcriptAreaStyle.Render(m.transcript.View())}
	if status := m.statusView(); status != "" {
		parts = append(parts, status)
	}

	parts = append(parts, m.composer.View())

	return pageStyle.Render(strings.Join(parts, sectionSeparator))
}

func (m *Model) resize() {
	width := m.contentWidth()
	m.composer.SetWidth(width)

	statusHeight := 0
	if status := m.statusView(); status != "" {
		statusHeight = lipgloss.Height(status)
	}

	transcriptHeight := m.height -
		pageStyle.GetVerticalFrameSize() -
		transcriptAreaStyle.GetVerticalFrameSize() -
		m.composer.Height() -
		statusHeight
	m.transcript.SetSize(width, max(transcriptHeight, minimumContentSize))
}

func (m *Model) status() string {
	if m.err != nil {
		return errorPrefix + m.err.Error()
	}

	return ""
}

func (m *Model) statusView() string {
	status := m.status()
	if status == "" {
		return ""
	}

	return errorStyle.Width(m.contentWidth()).Render(status)
}

func (m *Model) contentWidth() int {
	return max(m.width-pageStyle.GetHorizontalFrameSize(), minimumContentSize)
}
