package report

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	headingStyle   = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	valueStyle     = lipgloss.NewStyle().Foreground(theme.Default.Text).Bold(true)
	detailStyle    = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	countStyle     = lipgloss.NewStyle().Foreground(theme.Default.Muted)
	connectedStyle = lipgloss.NewStyle().Foreground(theme.Default.Success)
	warningStyle   = lipgloss.NewStyle().Foreground(theme.Default.Warning)
	failureStyle   = lipgloss.NewStyle().Foreground(theme.Default.Error)
)
