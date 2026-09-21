package hints

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderIncludesHintsAndFitsWidth(t *testing.T) {
	view := Render(40,
		Hint{Key: "Enter", Label: "Open"},
		Hint{Key: "Esc", Label: "Back"},
	)

	if !strings.Contains(view, "Enter") || !strings.Contains(view, "Back") {
		t.Fatalf("Render() = %q, want both hints", view)
	}

	if width := lipgloss.Width(view); width != 40 {
		t.Fatalf("Render() width = %d, want 40", width)
	}
}
