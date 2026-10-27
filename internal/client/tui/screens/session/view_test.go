package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/config"
)

func TestViewShowsAgentContextAndUsage(t *testing.T) {
	for _, thinking := range []string{"", "high"} {
		t.Run("reasoning="+thinking, func(t *testing.T) {
			screen := New(context.Background(), nil, config.Config{
				ProviderID: "openai",
				ModelID:    "test-model",
				Generation: config.Generation{Thinking: thinking},
			}, "session-1")
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
			screen.sessionUsage = kit.Usage{InputTokens: 3400, OutputTokens: 900}

			if thinking == "" {
				thinking = "default"
			}

			view := screen.View().Content
			for _, want := range []string{
				"openai / test-model",
				thinking + " reasoning",
				"3.4k in / 900 out",
				"Shift+Enter",
				"Ctrl+P",
				"Ctrl+C",
			} {
				if !strings.Contains(view, want) {
					t.Fatalf("view is missing %q: %q", want, view)
				}
			}
		})
	}
}

func TestViewFitsWindowWithContext(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {80, 20}, {120, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			screen := New(context.Background(), nil, config.Config{
				ProviderID: "openai",
				ModelID:    "test-model",
				Generation: config.Generation{Thinking: "high"},
			}, "session-1")
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen.sessionUsage = kit.Usage{InputTokens: 1200, OutputTokens: 300}
			screen.applyEvent(executionEvent(t, execution.StartedEntryType, "execution-1", execution.Started{}))

			view := screen.View().Content
			if width, height := lipgloss.Width(view), lipgloss.Height(view); width != size[0] || height != size[1] {
				t.Fatalf("view = %dx%d, want %dx%d: %q", width, height, size[0], size[1], view)
			}

			if !strings.Contains(view, "Esc") {
				t.Fatal("running execution has no interrupt hint")
			}
		})
	}
}
