package session

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/internal/service"
)

func TestPromptAnswerUsesResponseEndpointAndPreservesDraft(t *testing.T) {
	app := &promptService{}

	screen := New(t.Context(), app, "session")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen.composer.Focus()
	screen.composer.Update(tea.PasteMsg{Content: "another request"})

	request := interaction.Request{ID: "request", ExecutionID: "execution", Question: interaction.Question{
		Text: "Which deadline?", Choices: []interaction.Choice{{ID: "soon", Label: "As soon as possible"}},
	}}
	screen.applyEvent(executionEvent(t, interaction.RequestedEntryType, request.ExecutionID, request))

	view := ansi.Strip(screen.View().Content)
	if !strings.Contains(view, request.Question.Text) || strings.Contains(view, "╭") || lipgloss.Height(view) != 24 {
		t.Fatalf("view = %q", view)
	}

	_, command := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, command = screen.Update(command())
	screen.Update(command())

	if app.sessionID != "session" || app.requestID != request.ID || app.answer.ChoiceID != "soon" {
		t.Fatalf("response was routed incorrectly: %+v", app)
	}

	if screen.prompt.Active() || screen.composer.Value() != "another request" {
		t.Fatalf("response left pending prompts or changed the draft: %+v, %q", screen.prompt.Current(), screen.composer.Value())
	}
}

func TestPromptReplayAndExecutionInterruption(t *testing.T) {
	request := interaction.Request{ID: "request", ExecutionID: "execution", Question: interaction.Question{Text: "Which deadline?"}}

	screen := New(t.Context(), nil, "session")
	defer screen.Close()

	screen.applyEvent(executionEvent(t, interaction.RequestedEntryType, request.ExecutionID, request))
	screen.applyEvent(executionEvent(t, interaction.AnsweredEntryType, request.ExecutionID, interaction.Response{
		RequestID: request.ID, Answer: interaction.Answer{Text: "Friday"},
	}))

	if screen.prompt.Active() {
		t.Fatal("replaying a resolved question left it answerable")
	}

	screen.applyEvent(executionEvent(t, interaction.RequestedEntryType, request.ExecutionID, request))
	screen.applyEvent(executionEvent(t, execution.EntryType, request.ExecutionID, execution.Record{Status: execution.Interrupted}))

	if screen.prompt.Active() {
		t.Fatal("a question from an interrupted execution remained answerable")
	}
}

func TestHidingPromptDoesNotCancelWork(t *testing.T) {
	app := &promptService{}

	screen := New(t.Context(), app, "session")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	request := interaction.Request{ID: "request", ExecutionID: "execution", Question: interaction.Question{
		Text: "Which deadline?", Choices: []interaction.Choice{{ID: "soon", Label: "Soon"}},
	}}
	screen.applyEvent(executionEvent(t, interaction.RequestedEntryType, request.ExecutionID, request))
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !screen.prompt.Active() || screen.prompt.Expanded() || app.cancelledExecution != "" {
		t.Fatal("Esc cancelled work instead of collapsing the question")
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if !screen.prompt.Expanded() {
		t.Fatal("Enter did not reopen the question")
	}

	_, command := screen.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	_, command = screen.Update(command())
	screen.Update(command())

	if app.cancelledExecution != request.ExecutionID {
		t.Fatalf("explicit cancellation targeted %q", app.cancelledExecution)
	}
}

func TestPromptFitsSmallWindowsInEachMode(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 32}} {
		screen := New(t.Context(), nil, "session")
		defer screen.Close()

		screen.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

		request := interaction.Request{ID: "request", ExecutionID: "execution", Question: interaction.Question{
			Text: strings.Repeat("Some important context. ", 20), Choices: []interaction.Choice{
				{ID: "first", Label: "First option", Description: "Description of the first option."},
				{ID: "second", Label: "Second option"},
			},
		}}
		screen.applyEvent(executionEvent(t, interaction.RequestedEntryType, request.ExecutionID, request))

		for _, message := range []tea.Msg{nil, tea.PasteMsg{Content: "My answer"}, tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEscape}} {
			if message != nil {
				screen.Update(message)
			}

			view := screen.View().Content
			if lipgloss.Width(view) != size[0] || lipgloss.Height(view) != size[1] {
				t.Fatalf("prompt view = %dx%d, want %dx%d", lipgloss.Width(view), lipgloss.Height(view), size[0], size[1])
			}
		}
	}
}

type promptService struct {
	service.Service
	sessionID          string
	requestID          string
	answer             interaction.Answer
	cancelledExecution string
}

func (s *promptService) Respond(_ context.Context, sessionID, requestID string, answer interaction.Answer) error {
	s.sessionID, s.requestID, s.answer = sessionID, requestID, answer

	return nil
}

func (s *promptService) Cancel(_ context.Context, _, executionID string) error {
	s.cancelledExecution = executionID

	return nil
}
