package contextview

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	pageStyle     = lipgloss.NewStyle().Padding(0, 2)
	titleStyle    = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	textStyle     = lipgloss.NewStyle().Foreground(theme.Default.Text)
	subtleStyle   = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	selectedStyle = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(theme.Default.Error)
)
