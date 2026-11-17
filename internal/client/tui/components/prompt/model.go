package prompt

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/harness/interaction"
)

type mode int

const (
	choosing mode = iota
	editing
	collapsed
)

type Submit struct {
	RequestID string
	Answer    interaction.Answer
}

type Cancel struct {
	RequestID string
}

type Model struct {
	requests        []interaction.Request
	input           textarea.Model
	body            viewport.Model
	mode            mode
	resume          mode
	selected        int
	submitting      bool
	followSelection bool
	width           int
	height          int
}

func New() Model {
	input := textarea.New()
	input.Prompt = "> "
	input.Placeholder = "Type your answer…"
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight, input.MaxHeight = 1, 5
	input.SetPromptFunc(len(input.Prompt), func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "> "
		}

		return ""
	})
	input.SetStyles(editorStyles(input.Styles()))

	body := viewport.New()
	body.SoftWrap = false

	return Model{input: input, body: body, width: 80, height: 16}
}

func (m Model) Active() bool {
	return len(m.requests) > 0
}

func (m Model) Expanded() bool {
	return m.Active() && m.mode != collapsed
}

func (m Model) Current() interaction.Request {
	if !m.Active() {
		return interaction.Request{}
	}

	return m.requests[0]
}

func (m *Model) Add(request interaction.Request) tea.Cmd {
	if slices.ContainsFunc(m.requests, func(current interaction.Request) bool { return current.ID == request.ID }) {
		return nil
	}

	m.requests = append(m.requests, request)
	if len(m.requests) == 1 {
		return m.start()
	}

	return nil
}

func (m *Model) Remove(requestID string) tea.Cmd {
	index := slices.IndexFunc(m.requests, func(request interaction.Request) bool { return request.ID == requestID })
	if index < 0 {
		return nil
	}

	m.requests = slices.Delete(m.requests, index, index+1)
	if index == 0 {
		return m.start()
	}

	return nil
}

func (m *Model) Interrupt(executionID string) {
	previous := m.Current().ID

	m.requests = slices.DeleteFunc(m.requests, func(request interaction.Request) bool { return request.ExecutionID == executionID })
	if m.Current().ID != previous {
		m.start()
	}
}

func (m *Model) BeginSubmit(requestID string) bool {
	if !m.Active() || m.Current().ID != requestID || m.submitting {
		return false
	}

	m.submitting = true

	return true
}

func (m *Model) EndSubmit(requestID string) {
	if m.Current().ID == requestID {
		m.submitting = false
	}
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(1, width), max(5, height)
	m.input.SetWidth(max(1, m.width-surfaceStyle.GetHorizontalFrameSize()))
	available := m.height - surfaceStyle.GetVerticalFrameSize() - lipgloss.Height(m.caption())

	separator := 0
	if available >= 3 {
		separator = 1
	}

	m.input.MaxHeight = max(1, available-1-separator)
}

func (m *Model) Update(message tea.Msg) tea.Cmd {
	if !m.Active() {
		return nil
	}

	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+x":
			requestID := m.Current().ID

			return func() tea.Msg { return Cancel{RequestID: requestID} }
		case "esc":
			if m.mode == editing && len(m.Current().Question.Choices) > 0 {
				m.mode = choosing
				m.selected = len(m.Current().Question.Choices)
				m.followSelection = true
			} else if m.mode != collapsed {
				m.resume, m.mode = m.mode, collapsed
			}

			return m.focus()
		}

		if m.mode == collapsed {
			if key.String() == "enter" {
				m.mode = m.resume

				return m.focus()
			}

			return nil
		}

		switch key.String() {
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			return m.scroll(message)
		case "shift+enter":
			if m.mode == editing {
				m.input.InsertRune('\n')
			}

			return nil
		case "enter":
			if m.submitting {
				return nil
			}

			choices := m.Current().Question.Choices
			if m.mode == choosing && m.selected == len(choices) {
				return m.edit()
			}

			answer := interaction.Answer{}
			if m.mode == choosing {
				answer.ChoiceID = choices[m.selected].ID
			} else {
				answer.Text = strings.TrimSpace(m.input.Value())
				if answer.Text == "" {
					return nil
				}
			}

			requestID := m.Current().ID

			return func() tea.Msg { return Submit{RequestID: requestID, Answer: answer} }
		}

		if m.mode == choosing {
			switch key.String() {
			case "up", "down", "tab":
				count := len(m.Current().Question.Choices) + 1

				delta := 1
				if key.String() == "up" {
					delta = -1
				}

				m.selected = (m.selected + delta + count) % count
				m.followSelection = true

				return nil
			}

			if key.Text != "" {
				m.edit()
			}
		}
	}

	if m.mode == collapsed {
		return nil
	}

	if _, ok := message.(tea.MouseWheelMsg); ok {
		return m.scroll(message)
	}

	if _, ok := message.(tea.PasteMsg); ok {
		m.edit()
	}

	if m.mode == editing {
		var command tea.Cmd

		m.input, command = m.input.Update(message)

		return command
	}

	return nil
}

func (m *Model) start() tea.Cmd {
	m.input.Reset()
	m.selected, m.submitting, m.followSelection = 0, false, false

	m.mode = choosing
	if len(m.Current().Question.Choices) == 0 {
		m.mode = editing
	}

	m.body.GotoTop()

	return m.focus()
}

func (m *Model) edit() tea.Cmd {
	m.mode = editing
	m.body.GotoTop()

	return m.focus()
}

func (m *Model) focus() tea.Cmd {
	if m.Active() && m.mode == editing {
		return m.input.Focus()
	}

	m.input.Blur()

	return nil
}

func (m *Model) scroll(message tea.Msg) tea.Cmd {
	var command tea.Cmd

	m.body, command = m.body.Update(message)

	return command
}
