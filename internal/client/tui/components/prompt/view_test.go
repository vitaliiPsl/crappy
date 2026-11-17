package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

func TestPromptFillsComposerBackground(t *testing.T) {
	for _, state := range []string{"choices", "editing", "multiline", "collapsed", "queued", "submitting"} {
		t.Run(state, func(t *testing.T) {
			model := New()
			model.Add(interaction.Request{ID: "request", Question: interaction.Question{
				Text: "Which topic?", Choices: []interaction.Choice{
					{ID: "architecture", Label: "Architecture", Description: "Trace components and data flow."},
					{ID: "review", Label: "Code review"},
				},
			}})
			model.SetSize(60, 16)

			switch state {
			case "editing":
				model.edit()
			case "multiline":
				model.edit()
				model.input.SetValue("Review the architecture\nand explain the data flow")
			case "collapsed":
				model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			case "queued":
				model.Add(interaction.Request{ID: "next"})
			case "submitting":
				model.BeginSubmit("request")
			}

			view := model.View()
			screen := uv.NewScreenBuffer(60, lipgloss.Height(view))
			uv.NewStyledString(view).Draw(screen, screen.Bounds())

			red, green, blue, alpha := theme.Default.SurfaceAlt.RGBA()
			for y := 0; y < screen.Height(); y++ {
				for x := 0; x < screen.Width(); x++ {
					cell := screen.CellAt(x, y)
					if cell == nil || cell.Style.Bg == nil {
						t.Fatalf("composer background missing at (%d, %d)", x, y)
					}

					r, g, b, a := cell.Style.Bg.RGBA()
					if r != red || g != green || b != blue || a != alpha {
						t.Fatalf("composer background at (%d, %d) = %v", x, y, cell.Style.Bg)
					}
				}
			}
		})
	}
}

func TestPromptKeepsEditorVisibleWhileBodyScrolls(t *testing.T) {
	model := New()
	model.Add(interaction.Request{ID: "request", Question: interaction.Question{
		Text: "Beginning of context\n" + strings.Repeat("More context\n", 30) + "End of context",
	}})
	model.SetSize(40, 10)

	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Beginning of context") || !strings.Contains(view, "Type your answer") {
		t.Fatalf("initial panel hides context or editor: %q", view)
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})

	view = ansi.Strip(model.View())
	if strings.Contains(view, "Beginning of context") || !strings.Contains(view, "Type your answer") {
		t.Fatalf("scrolling moved the editor: %q", view)
	}

	if lipgloss.Width(view) != 40 || lipgloss.Height(view) > 10 {
		t.Fatalf("panel dimensions = %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	if !strings.Contains(model.View(), "Beginning of context") {
		t.Fatal("the beginning of the question cannot be scrolled back into view")
	}
}

func TestPanelShowsOnlySelectedDescription(t *testing.T) {
	model := New()
	model.Add(interaction.Request{ID: "request", Question: interaction.Question{
		Text: "Which topic?", Choices: []interaction.Choice{
			{ID: "architecture", Label: "Architecture", Description: "Trace components and data flow."},
			{ID: "review", Label: "Code review", Description: "Review correctness and concurrency risks."},
		},
	}})
	model.SetSize(80, 16)

	if view := model.View(); !strings.Contains(view, "Trace components") || strings.Contains(view, "Review correctness") {
		t.Fatalf("unselected descriptions are visible: %q", view)
	}

	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if view := model.View(); strings.Contains(view, "Trace components") || !strings.Contains(view, "Review correctness") {
		t.Fatalf("selection did not update the description: %q", view)
	}
}
