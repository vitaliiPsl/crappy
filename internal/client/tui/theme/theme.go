package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type Theme struct {
	Primary color.Color
	Text    color.Color
	Subtle  color.Color
	Muted   color.Color

	Error   color.Color
	Success color.Color
	Warning color.Color

	SurfaceAlt color.Color
}

var Default = Theme{
	Primary: lipgloss.Color("#89DDFF"),
	Text:    lipgloss.Color("#EEFFFF"),
	Subtle:  lipgloss.Color("#B0B0B0"),
	Muted:   lipgloss.Color("#777777"),

	Error:   lipgloss.Color("#FF5370"),
	Success: lipgloss.Color("#C3E88D"),
	Warning: lipgloss.Color("#FFCB6B"),

	SurfaceAlt: lipgloss.Color("#555662"),
}
