package contextview

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInspectorViewFitsNarrowAndShortWindows(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{1, 1}, {8, 4}, {32, 10}, {80, 24}} {
		screen := New(context.Background(), nil, "session-1")
		screen.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		screen.Update(loadedMsg{target: screen, data: inspectionFixture()})

		for _, detail := range []bool{false, true} {
			if detail {
				screen.showDetail("Definition", strings.Repeat("long tool description ", 100), 100)
			}

			view := screen.View().Content
			if lipgloss.Height(view) > size.height {
				t.Fatalf("view height exceeds %d: %q", size.height, view)
			}

			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size.width {
					t.Fatalf("view width exceeds %d: %q", size.width, line)
				}
			}
		}

		screen.Close()
	}
}

func TestInspectorShowsEstimatedInputSize(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	screen.Update(loadedMsg{target: screen, data: inspectionFixture()})

	view := ansi.Strip(screen.View().Content)
	for _, text := range []string{"≈100 tokens", "/workspace", "Core", "Environment", "Conversation · 1 message", "Tools · 1 definition"} {
		if !strings.Contains(view, text) {
			t.Fatalf("overview omitted %q: %s", text, view)
		}
	}
}

func TestInspectorStripsTerminalEscapesFromContent(t *testing.T) {
	t.Parallel()

	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen.Update(loadedMsg{target: screen, data: inspectionFixture()})
	screen.showDetail("Instruction", "before\x1b[2Jafter", 10)

	if view := screen.View().Content; strings.Contains(view, "\x1b[2J") || !strings.Contains(ansi.Strip(view), "beforeafter") {
		t.Fatalf("unsafe content was rendered: %q", view)
	}
}
