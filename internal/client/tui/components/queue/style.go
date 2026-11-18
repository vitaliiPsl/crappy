package queue

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	headingStyle  = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	textStyle     = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	selectedStyle = lipgloss.NewStyle().Foreground(theme.Default.Text).Bold(true)
)
