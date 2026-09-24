package transcript

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	mutedStyle = lipgloss.NewStyle().Foreground(theme.Default.Subtle)

	userStyle = lipgloss.NewStyle().
			Foreground(theme.Default.Text).
			Background(theme.Default.SurfaceAlt).
			Padding(1)
	assistantStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	thinkingStyle  = lipgloss.NewStyle().
			Foreground(theme.Default.Muted).
			Italic(true)

	toolNameStyle = lipgloss.NewStyle().Foreground(theme.Default.Text).Bold(true)
	toolBaseStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			PaddingLeft(1)
	toolPendingStyle = toolBaseStyle.BorderForeground(theme.Default.Warning)
	toolSuccessStyle = toolBaseStyle.BorderForeground(theme.Default.Success)
	toolErrorStyle   = toolBaseStyle.BorderForeground(theme.Default.Error)

	checkpointStyle        = lipgloss.NewStyle().Foreground(theme.Default.Muted)
	checkpointPendingStyle = lipgloss.NewStyle().Foreground(theme.Default.Primary)

	emptyTitleStyle = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	emptyTextStyle  = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
)
