package session

import (
	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

const (
	pagePaddingHorizontal  = 2
	headerMarginBottom     = 1
	transcriptMarginBottom = 1
)

var (
	pageStyle = lipgloss.NewStyle().
			PaddingLeft(pagePaddingHorizontal).
			PaddingRight(pagePaddingHorizontal)
	transcriptAreaStyle = lipgloss.NewStyle().MarginBottom(transcriptMarginBottom)
	headerStyle         = lipgloss.NewStyle().Foreground(theme.Default.Subtle).MarginBottom(headerMarginBottom)
	errorStyle          = lipgloss.NewStyle().Foreground(theme.Default.Error)
	workingStyle        = lipgloss.NewStyle().Foreground(theme.Default.Primary)
	noticeStyle         = lipgloss.NewStyle().Foreground(theme.Default.Warning)

	contextHeadingStyle = lipgloss.NewStyle().Foreground(theme.Default.Primary).Bold(true)
	contextValueStyle   = lipgloss.NewStyle().Foreground(theme.Default.Text).Bold(true)
	contextDetailStyle  = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	contextCountStyle   = lipgloss.NewStyle().Foreground(theme.Default.Muted)
)
