package contextview

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
)

func TestInspectorNavigatesSectionsAndReturnsToSelection(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen.Update(loadedMsg{target: screen, data: inspectionFixture()})
	screen.selected = 2
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.mode != detailView || screen.detailTitle != "Environment" || !strings.Contains(screen.View().Content, "Working directory") {
		t.Fatalf("instruction details missing: %s", screen.View().Content)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if screen.mode != overviewView || screen.selected != 2 {
		t.Fatal("back did not restore the overview selection")
	}

	screen.selected = 3
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.mode != messagesView || !strings.Contains(screen.View().Content, "user") {
		t.Fatalf("conversation list missing: %s", screen.View().Content)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.mode != detailView || !strings.Contains(screen.View().Content, "Hello from the conversation") {
		t.Fatalf("message body missing: %s", screen.View().Content)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	screen.selected = 4
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.mode != detailView || !strings.Contains(screen.View().Content, "timeout_seconds") {
		t.Fatalf("decorated tool schema missing: %s", screen.View().Content)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	_, command := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil || command().(navigation.Action).Operation() != navigation.OperationBack {
		t.Fatal("Escape from overview did not return to the conversation")
	}
}

func TestInspectorScrollsLongListsAndDetails(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	data := inspectionFixture()

	data.Messages = make([]assistant.ContextMessage, 30)
	for index := range data.Messages {
		data.Messages[index] = assistant.ContextMessage{Message: kit.NewUserMessage(kit.NewTextContent("message")), Tokens: 10}
	}

	screen.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	screen.Update(loadedMsg{target: screen, data: data})
	screen.selected = 3
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnd})

	if screen.selected != 29 || screen.offset == 0 || !strings.Contains(screen.View().Content, "30 · user") {
		t.Fatalf("selected message is not visible: %s", screen.View().Content)
	}

	screen.showDetail("Long section", strings.Repeat("first page\n", 20)+strings.Repeat("last page\n", 20), 100)

	before := screen.View().Content
	for range 4 {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}

	if after := screen.View().Content; before == after || !strings.Contains(after, "last page") {
		t.Fatalf("details did not scroll: %s", after)
	}
}

func TestInspectorIgnoresOtherOrClosedScreenResults(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	screen.Update(loadedMsg{data: inspectionFixture()})

	if !screen.loading {
		t.Fatal("another screen's result was accepted")
	}

	screen.Close()
	screen.Update(loadedMsg{target: screen, data: inspectionFixture()})

	if !screen.loading {
		t.Fatal("a closed screen accepted a result")
	}
}

func inspectionFixture() assistant.ContextReport {
	return assistant.ContextReport{
		Workdir: "/workspace",
		Instructions: []assistant.ContextInstruction{
			{Text: "Be concise.", Tokens: 10},
			{Source: "Environment", Text: "# Environment\n\nWorking directory /workspace", Tokens: 20},
		},
		Messages: []assistant.ContextMessage{{Message: kit.NewUserMessage(kit.NewTextContent("Hello from the conversation")), Tokens: 30}},
		Tools: []assistant.ContextTool{{ToolDefinition: kit.ToolDefinition{
			Name: "bash", Description: "Run a shell command", Schema: map[string]any{
				"type": "object", "properties": map[string]any{"timeout_seconds": map[string]any{"type": "integer"}},
			},
		}, Tokens: 40}},
	}
}
