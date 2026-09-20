package composer

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var composerStyle = lipgloss.NewStyle().Background(theme.Default.SurfaceAlt)

func composerStyles(styles textarea.Styles) textarea.Styles {
	state := textarea.StyleState{
		Base:        lipgloss.NewStyle().Background(theme.Default.SurfaceAlt),
		Text:        lipgloss.NewStyle().Foreground(theme.Default.Text),
		Placeholder: lipgloss.NewStyle().Foreground(theme.Default.Subtle),
		Prompt:      lipgloss.NewStyle().Foreground(theme.Default.Primary),
		CursorLine:  lipgloss.NewStyle().Background(theme.Default.SurfaceAlt),
		EndOfBuffer: lipgloss.NewStyle().Foreground(theme.Default.SurfaceAlt),
	}

	styles.Focused = state
	styles.Blurred = state

	return styles
}
