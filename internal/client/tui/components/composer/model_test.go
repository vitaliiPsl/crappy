package composer

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestSubmitPreservesInputUntilReset(t *testing.T) {
	model := New()
	model.input.SetValue("hello")

	command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	message, ok := command().(Submit)

	if !ok || message.Text != "hello" {
		t.Fatalf("message = %#v, want Submit hello", message)
	}

	if model.input.Value() != "hello" {
		t.Fatalf("input = %q, want hello", model.input.Value())
	}

	model.Reset()

	if model.input.Value() != "" {
		t.Fatalf("input = %q, want empty", model.input.Value())
	}
}

func TestViewUsesAvailableWidth(t *testing.T) {
	model := New()
	model.SetWidth(80)

	if width := lipgloss.Width(model.View()); width != 80 {
		t.Fatalf("view width = %d, want 80", width)
	}
}
