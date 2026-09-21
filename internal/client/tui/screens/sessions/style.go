package sessions

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

const (
	pagePaddingHorizontal = 2
)

var (
	pageStyle = lipgloss.NewStyle().
			PaddingLeft(pagePaddingHorizontal).
			PaddingRight(pagePaddingHorizontal)
	titleStyle = lipgloss.NewStyle().
			Foreground(theme.Default.Primary).
			Bold(true)
	selectedStyle = lipgloss.NewStyle().
			Foreground(theme.Default.Primary).
			Bold(true)
	selectedSessionStyle = selectedStyle
	currentStyle         = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	messageStyle         = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	warningStyle         = lipgloss.NewStyle().Foreground(theme.Default.Warning)
	errorStyle           = lipgloss.NewStyle().Foreground(theme.Default.Error)
	sessionStyle         = lipgloss.NewStyle().Foreground(theme.Default.Text)
)
