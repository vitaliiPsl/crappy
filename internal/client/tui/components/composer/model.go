package composer

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type Submit struct {
	Text string
}

type Model struct {
	input textarea.Model
}

func New() Model {
	input := textarea.New()
	input.Prompt = "> "
	input.Placeholder = "Type a message..."
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = 8

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
	return strings.TrimRight(m.input.View(), "\n")
}

func (m Model) Height() int {
	return m.input.Height()
}

func (m *Model) SetWidth(width int) {
	m.input.SetWidth(max(width, 1))
}

func (m *Model) Reset() {
	m.input.Reset()
}
