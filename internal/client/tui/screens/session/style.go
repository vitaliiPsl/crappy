package session

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

const (
	pagePaddingHorizontal  = 2
	transcriptMarginBottom = 1
)

var (
	pageStyle = lipgloss.NewStyle().
			PaddingLeft(pagePaddingHorizontal).
			PaddingRight(pagePaddingHorizontal)
	transcriptAreaStyle = lipgloss.NewStyle().MarginBottom(transcriptMarginBottom)
	errorStyle          = lipgloss.NewStyle().Foreground(theme.Default.Error)
	workingStyle        = lipgloss.NewStyle().Foreground(theme.Default.Primary)
	noticeStyle         = lipgloss.NewStyle().Foreground(theme.Default.Warning)
)
