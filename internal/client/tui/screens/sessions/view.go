package sessions

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

const (
	title            = "Sessions"
	loadingLabel     = "Loading…"
	emptyLabel       = "No sessions yet. Press n to start one."
	errorPrefix      = "Error: "
	deletePrompt     = "Delete %q?"
	deletingLabel    = "Deleting session…"
	currentSuffix    = "  Current"
	selectedMarker   = "› "
	unselectedMarker = "  "
	screenGaps       = 2
	minimumViewSize  = 1
)

var defaultHints = []hints.Hint{
	{Key: "↑/↓", Label: "Select"},
	{Key: "Enter", Label: "Open"},
	{Key: "n", Label: "New"},
	{Key: "d", Label: "Delete"},
	{Key: "Esc", Label: "Back"},
}

var deleteHints = []hints.Hint{
	{Key: "Enter", Label: "Delete"},
	{Key: "Esc", Label: "Cancel"},
}

func (m *Model) render() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.headerView(),
		"",
		m.listView(),
		"",
		m.footerView(),
	)

	return pageStyle.
		Width(max(m.width, minimumViewSize)).
		Height(max(m.height, minimumViewSize)).
		Render(content)
}

func (m *Model) headerView() string {
	return titleStyle.Width(m.contentWidth()).Render(title)
}

func (m *Model) footerView() string {
	if m.deleting {
		return messageStyle.
			Width(m.contentWidth()).
			Align(lipgloss.Center).
			Render(deletingLabel)
	}

	if m.deleteID != "" {
		return lipgloss.JoinVertical(
			lipgloss.Center,
			warningStyle.
				Width(m.contentWidth()).
				Align(lipgloss.Center).
				Render(fmt.Sprintf(deletePrompt, m.deleteID)),
			hints.Render(m.contentWidth(), deleteHints...),
		)
	}

	footer := hints.Render(m.contentWidth(), defaultHints...)
	if m.err == nil {
		return footer
	}

	return lipgloss.JoinVertical(
		lipgloss.Center,
		errorStyle.
			Width(m.contentWidth()).
			Align(lipgloss.Center).
			Render(errorPrefix+m.err.Error()),
		footer,
	)
}

func (m *Model) listView() string {
	if m.loading {
		return m.messageView(loadingLabel)
	}

	if len(m.sessions) == 0 {
		return m.messageView(emptyLabel)
	}

	end := min(m.offset+m.visibleRows(), len(m.sessions))
	rows := make([]string, 0, end-m.offset)

	for index := m.offset; index < end; index++ {
		rows = append(rows, m.sessionView(index))
	}

	return strings.Join(rows, "\n")
}

func (m *Model) sessionView(index int) string {
	value := m.sessions[index]
	marker := unselectedMarker
	style := sessionStyle

	if index == m.selected {
		marker = selectedStyle.Render(selectedMarker)
		style = selectedSessionStyle
	}

	label := style.Render(value.ID)
	if value.ID == m.currentID {
		label += currentStyle.Render(currentSuffix)
	}

	return marker + label
}

func (m *Model) messageView(message string) string {
	return messageStyle.
		Width(m.contentWidth()).
		Height(m.visibleRows()).
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(message)
}

func (m *Model) visibleRows() int {
	chromeHeight := lipgloss.Height(m.headerView()) +
		lipgloss.Height(m.footerView()) +
		screenGaps

	return max(
		m.height-pageStyle.GetVerticalFrameSize()-chromeHeight,
		minimumViewSize,
	)
}

func (m *Model) contentWidth() int {
	return max(m.width-pageStyle.GetHorizontalFrameSize(), minimumViewSize)
}
