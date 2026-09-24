package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/memory"
	harnesssession "github.com/vitaliipsl/crappy/harness/session"
)

func TestEventsUpdateTranscript(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	input, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent("hello")))
	if err != nil {
		t.Fatal(err)
	}

	model.applyEvent(event.Event{Type: memory.EntryType, Payload: input})

	agentEvent, err := json.Marshal(
		kit.NewAgentContentDeltaEvent(kit.NewTextContent("world")),
	)
	if err != nil {
		t.Fatal(err)
	}

	model.applyEvent(event.Event{
		Type:    harnesssession.AgentEventType,
		Payload: agentEvent,
	})

	view := model.View().Content
	if !strings.Contains(view, "hello") || !strings.Contains(view, "world") {
		t.Fatalf("view = %q, want durable and streaming messages", view)
	}
}

func TestViewFitsWindow(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	view := model.View().Content
	if width, height := lipgloss.Width(view), lipgloss.Height(view); width != 80 || height != 20 {
		t.Fatalf("view = %dx%d, want 80x20", width, height)
	}
}

func executionEvent(t *testing.T, eventType, executionID string, payload any) event.Event {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	return event.Event{
		Type:        eventType,
		ExecutionID: executionID,
		Payload:     encoded,
	}
}

func TestExecutionLifecycleDrivesStatus(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	if model.running() {
		t.Fatal("running() = true before any execution")
	}

	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionStartedEntryType,
		"execution-1",
		harnesssession.ExecutionStarted{},
	))

	if !model.running() {
		t.Fatal("running() = false while an execution is running")
	}

	if !strings.Contains(model.View().Content, workingLabel) {
		t.Fatalf("view = %q, want working status", model.View().Content)
	}

	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionFinishedEntryType,
		"execution-1",
		harnesssession.ExecutionFinished{Outcome: harnesssession.ExecutionCompleted},
	))

	if model.running() {
		t.Fatal("running() = true after the execution finished")
	}

	if status := model.statusView(); status != "" {
		t.Fatalf("statusView() = %q, want empty after a completed execution", status)
	}
}

func TestFailedExecutionIsSurfaced(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionStartedEntryType,
		"execution-1",
		harnesssession.ExecutionStarted{},
	))
	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionFinishedEntryType,
		"execution-1",
		harnesssession.ExecutionFinished{
			Outcome: harnesssession.ExecutionFailed,
			Error:   "model exploded",
		},
	))

	if !strings.Contains(model.View().Content, "model exploded") {
		t.Fatalf("view = %q, want the execution error", model.View().Content)
	}
}

func TestInterruptedExecutionIsSurfaced(t *testing.T) {
	for _, outcome := range []harnesssession.ExecutionOutcome{
		harnesssession.ExecutionCancelled,
		harnesssession.ExecutionInterrupted,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			model := New(context.Background(), nil, "session-1")
			model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

			model.applyEvent(executionEvent(
				t,
				harnesssession.ExecutionFinishedEntryType,
				"execution-1",
				harnesssession.ExecutionFinished{Outcome: outcome},
			))

			if !strings.Contains(model.View().Content, interruptedLabel) {
				t.Fatalf("view = %q, want %q", model.View().Content, interruptedLabel)
			}
		})
	}
}

func TestSubmitIsIgnoredWhileBusy(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionStartedEntryType,
		"execution-1",
		harnesssession.ExecutionStarted{},
	))

	if command := model.submit("second message"); command != nil {
		t.Fatal("submit() returned a command while busy")
	}
}

func TestCancelKeyOnlyFiresWhileRunning(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	if _, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); command != nil {
		t.Fatal("Update() returned a command for the cancel key while idle")
	}

	model.applyEvent(executionEvent(
		t,
		harnesssession.ExecutionStartedEntryType,
		"execution-1",
		harnesssession.ExecutionStarted{},
	))

	if _, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); command == nil {
		t.Fatal("Update() returned no command for the cancel key while running")
	}
}

func TestMouseWheelScrollsTranscript(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	for i := range 40 {
		input, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(fmt.Sprintf("line %d", i))))
		if err != nil {
			t.Fatal(err)
		}

		model.applyEvent(event.Event{Type: memory.EntryType, Payload: input})
	}

	bottom := model.View().Content

	model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if model.View().Content == bottom {
		t.Fatal("mouse wheel up did not scroll the transcript")
	}
}

func TestCheckpointEventsUpdateTranscript(t *testing.T) {
	model := New(context.Background(), nil, "session-1")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model.applyEvent(event.Event{Type: checkpoint.StartedEventType, ExecutionID: "execution-1"})

	if view := model.View().Content; !strings.Contains(view, "Compacting context") {
		t.Fatalf("view = %q, want pending checkpoint", view)
	}

	payload, err := json.Marshal(checkpoint.Checkpoint{
		Summary:  checkpoint.Summary{Objective: "ship compaction"},
		Retained: []kit.Message{kit.NewUserMessage(kit.NewTextContent("kept"))},
	})
	if err != nil {
		t.Fatal(err)
	}

	model.applyEvent(event.Event{Type: checkpoint.EntryType, ExecutionID: "execution-1", Payload: payload})

	if view := model.View().Content; !strings.Contains(view, "Context compacted") {
		t.Fatalf("view = %q, want compacted checkpoint", view)
	}
}
