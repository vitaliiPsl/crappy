package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/harness/interaction"
)

func TestPromptAcceptsChoiceAndCustomAnswer(t *testing.T) {
	request := interaction.Request{ID: "request", Question: interaction.Question{
		Text: "Which deadline?", Choices: []interaction.Choice{{ID: "soon", Label: "As soon as possible"}},
	}}
	model := New()
	model.Add(request)
	model.SetSize(60, 10)

	command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	selected := command().(Submit)
	if selected.RequestID != request.ID || selected.Answer.ChoiceID != "soon" {
		t.Fatalf("selected answer = %+v", selected)
	}

	model.Update(tea.PasteMsg{Content: "Friday, under $15,000"})
	command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	custom := command().(Submit)
	if custom.Answer.Text != "Friday, under $15,000" || custom.Answer.ChoiceID != "" {
		t.Fatalf("custom answer = %+v", custom)
	}

	if view := model.View(); !strings.Contains(view, "Which deadline?") || lipgloss.Width(view) > 60 || lipgloss.Height(view) > 10 {
		t.Fatalf("prompt view = %q", view)
	}
}

func TestOpenQuestionDoesNotSubmitEmptyAnswer(t *testing.T) {
	model := New()
	model.Add(interaction.Request{ID: "request", Question: interaction.Question{Text: "Which deadline?"}})

	if command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); command != nil {
		t.Fatal("submitted an empty answer")
	}

	model.Update(tea.PasteMsg{Content: "Friday"})

	command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if answer := command().(Submit); answer.Answer.Text != "Friday" {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestPromptModesPreserveDraftAndAdvanceQuestions(t *testing.T) {
	model := New()
	first := interaction.Request{ID: "first", Question: interaction.Question{
		Text: "Which deadline?", Choices: []interaction.Choice{{ID: "soon", Label: "Soon"}},
	}}
	model.Add(first)
	model.Add(interaction.Request{ID: "second", Question: interaction.Question{Text: "Which format?"}})
	model.SetSize(60, 14)

	if !strings.Contains(model.View(), "Question 1 of 2") {
		t.Fatal("pending question count is missing")
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if model.mode != editing {
		t.Fatal("the custom-answer option did not open the editor")
	}

	model.Update(tea.PasteMsg{Content: "Friday"})
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if model.mode != choosing || model.input.Value() != "Friday" {
		t.Fatal("returning to choices discarded the answer draft")
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !model.Active() || model.Expanded() || !strings.Contains(model.View(), "Question waiting") {
		t.Fatal("hiding the question changed its pending state")
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if message := command().(Submit); message.Answer.Text != "Friday" {
		t.Fatalf("restored draft = %+v", message)
	}

	if !model.BeginSubmit(first.ID) || model.BeginSubmit(first.ID) {
		t.Fatal("duplicate answer submission was admitted")
	}

	model.Remove(first.ID)

	if model.Current().ID != "second" || model.mode != editing || model.input.Value() != "" {
		t.Fatal("next question inherited the previous answer state")
	}

	model.Remove("second")

	if model.Active() {
		t.Fatal("resolved questions left a prompt active")
	}
}
