package hints

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/theme"
)

const (
	separator       = "  •  "
	minimumViewSize = 1
)

var (
	keyStyle       = lipgloss.NewStyle().Foreground(theme.Default.Subtle)
	labelStyle     = lipgloss.NewStyle().Foreground(theme.Default.Muted)
	separatorStyle = lipgloss.NewStyle().Foreground(theme.Default.Muted)
)

type Hint struct {
	Key   string
	Label string
}

func Render(width int, values ...Hint) string {
	items := make([]string, 0, len(values))

	for _, value := range values {
		items = append(items, keyStyle.Render(value.Key)+" "+labelStyle.Render(value.Label))
	}

	return lipgloss.NewStyle().
		Width(max(width, minimumViewSize)).
		Align(lipgloss.Center).
		Render(strings.Join(items, separatorStyle.Render(separator)))
}
