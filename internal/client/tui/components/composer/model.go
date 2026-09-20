package composer

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	composerPrompt      = "> "
	composerPlaceholder = "Type a message… Enter to send"
	composerMinHeight   = 1
	composerMaxHeight   = 8
	composerPaddingX    = 1
	composerPaddingY    = 1
)

type Submit struct {
	Text string
}

type Model struct {
	input textarea.Model
	width int
}

func New() Model {
	input := textarea.New()
	input.Prompt = composerPrompt
	input.Placeholder = composerPlaceholder
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = composerMinHeight
	input.MaxHeight = composerMaxHeight
	input.SetPromptFunc(len(composerPrompt), func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return composerPrompt
		}

		return ""
	})
	input.SetStyles(composerStyles(input.Styles()))

	return Model{input: input}
}

func (m *Model) Init() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				return func() tea.Msg { return Submit{Text: text} }
			}
		case "shift+enter":
			m.input.InsertRune('\n')

			return nil
		}
	}

	var command tea.Cmd

	m.input, command = m.input.Update(message)

	return command
}

func (m Model) View() string {
	body := strings.TrimRight(m.input.View(), "\n")

	return composerStyle.
		Width(max(m.width, 1)).
		Padding(composerPaddingY, composerPaddingX).
		Render(body)
}

func (m Model) Height() int {
	return lipgloss.Height(m.View())
}

func (m *Model) SetWidth(width int) {
	m.width = max(width, 1)
	m.input.SetWidth(max(m.width-composerPaddingX*2, 1))
}

func (m *Model) Reset() {
	m.input.Reset()
}
