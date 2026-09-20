package session

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

var (
	pageStyle           = lipgloss.NewStyle().PaddingLeft(2).PaddingRight(2)
	transcriptAreaStyle = lipgloss.NewStyle().MarginBottom(1)
	errorStyle          = lipgloss.NewStyle().Foreground(theme.Default.Error)
)
