package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

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
