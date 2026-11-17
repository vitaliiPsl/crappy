package prompt

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	surfaceStyle     = lipgloss.NewStyle().Background(theme.Default.SurfaceAlt).Padding(1)
	titleStyle       = lipgloss.NewStyle().Foreground(theme.Default.Primary)
	labelStyle       = lipgloss.NewStyle().Foreground(theme.Default.Text)
	descriptionStyle = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	selectedStyle    = labelStyle.Bold(true)
)

func (m *Model) View() string {
	if !m.Active() {
		return ""
	}

	if m.mode == collapsed {
		width := max(1, m.width-surfaceStyle.GetHorizontalFrameSize())

		return surfaceStyle.Width(m.width).Render(titleStyle.Render(ansi.Truncate(
			"> Question waiting · Enter to answer", width, "…",
		)))
	}

	width := max(1, m.width-surfaceStyle.GetHorizontalFrameSize())
	body, selectedLine := m.bodyContent(width)

	editor := ""
	if m.mode == editing {
		editor = m.input.View()
	}

	caption := m.caption()
	chrome := surfaceStyle.GetVerticalFrameSize() + lipgloss.Height(caption)

	separator := "\n\n"
	if m.height-chrome < 3 {
		separator = "\n"
	}

	editorHeight := 0
	if editor != "" {
		editorHeight = lipgloss.Height(editor) + len(separator) - 1
	}

	height := max(1, min(lipgloss.Height(body), m.height-chrome-editorHeight))
	m.body.SetWidth(width)
	m.body.SetHeight(height)
	m.body.SetContent(body)

	if m.followSelection {
		offset := m.body.YOffset()
		if selectedLine < offset {
			m.body.SetYOffset(selectedLine)
		} else if selectedLine >= offset+height {
			m.body.SetYOffset(selectedLine - height + 1)
		}

		m.followSelection = false
	}

	content := m.body.View()
	if editor != "" {
		content += separator + editor
	}

	if caption != "" {
		content = descriptionStyle.Render(ansi.Truncate(caption, width, "…")) + "\n" + content
	}

	return surfaceStyle.Width(m.width).Render(content)
}

func (m Model) caption() string {
	title := ""
	if len(m.requests) > 1 {
		title = fmt.Sprintf("Question 1 of %d", len(m.requests))
	}

	if m.submitting {
		if title != "" {
			title += " · "
		}

		title += "Sending answer…"
	}

	return title
}

func (m Model) Shortcuts() string {
	values := []hints.Hint{}
	switch m.mode {
	case choosing:
		values = append(values, hints.Hint{Key: "↑/↓", Label: "Select"}, hints.Hint{Key: "Enter", Label: "Answer"}, hints.Hint{Key: "Esc", Label: "Hide"})
	case editing:
		back := "Hide"
		if len(m.Current().Question.Choices) > 0 {
			back = "Back"
		}

		values = append(values, hints.Hint{Key: "Enter", Label: "Answer"}, hints.Hint{Key: "Shift+Enter", Label: "Newline"}, hints.Hint{Key: "Esc", Label: back})
	case collapsed:
		values = append(values, hints.Hint{Key: "Enter", Label: "Answer"})
	}

	values = append(values, hints.Hint{Key: "Ctrl+X", Label: "Cancel work"})

	return hints.Render(m.width, values...)
}

func (m Model) bodyContent(width int) (string, int) {
	lines := []string{labelStyle.Background(theme.Default.SurfaceAlt).Width(width).
		Render(ansi.Hardwrap(ansi.Strip(m.Current().Question.Text), width, false))}

	selectedLine := 0
	if m.mode == choosing {
		lines = append(lines, "")
		for index := 0; index <= len(m.Current().Question.Choices); index++ {
			label := "Write your own answer…"
			if index < len(m.Current().Question.Choices) {
				label = m.Current().Question.Choices[index].Label
			}

			marker, style := "  ", descriptionStyle
			if index == m.selected {
				marker, style = "› ", selectedStyle
				selectedLine = lipgloss.Height(strings.Join(lines, "\n"))
			}

			label = strings.Join(strings.Fields(ansi.Strip(label)), " ")
			lines = append(lines, style.Background(theme.Default.SurfaceAlt).Width(width).
				Render(ansi.Hardwrap(marker+label, width, false)))
		}

		description := "Type an answer in your own words."
		if m.selected < len(m.Current().Question.Choices) {
			description = m.Current().Question.Choices[m.selected].Description
		}

		if description != "" {
			lines = append(lines, "", descriptionStyle.Background(theme.Default.SurfaceAlt).Width(width).
				Render(ansi.Hardwrap(ansi.Strip(description), width, false)))
		}
	}

	return strings.Join(lines, "\n"), selectedLine
}

func editorStyles(styles textarea.Styles) textarea.Styles {
	base := lipgloss.NewStyle().Background(theme.Default.SurfaceAlt)
	state := textarea.StyleState{
		Base: base, Text: labelStyle, Prompt: titleStyle, Placeholder: descriptionStyle,
		CursorLine: base, EndOfBuffer: lipgloss.NewStyle().Foreground(theme.Default.SurfaceAlt),
	}
	styles.Focused, styles.Blurred = state, state

	return styles
}
