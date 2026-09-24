package transcript

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
)

const (
	checkpointPendingLabel = "Compacting context…"
	checkpointLabel        = "Context compacted"
	checkpointRule         = "─"
)

func renderPendingCheckpoint(width int) string {
	return renderDivider(checkpointPendingLabel, checkpointPendingStyle, width)
}

func renderCheckpoint(generated checkpoint.Checkpoint, width int) string {
	label := fmt.Sprintf(
		"%s · %s → %s tokens · %s",
		checkpointLabel,
		tokensLabel(generated.TokensBefore),
		tokensLabel(generated.TokensAfter),
		retainedLabel(len(generated.Retained)),
	)

	return renderDivider(label, checkpointStyle, width)
}

func renderDivider(label string, style lipgloss.Style, width int) string {
	width = max(width-assistantStyle.GetHorizontalFrameSize(), 1)
	label = " " + label + " "

	fill := max(width-lipgloss.Width(label), 0)
	left := fill / 2

	divider := strings.Repeat(checkpointRule, left) + label + strings.Repeat(checkpointRule, fill-left)

	return assistantStyle.Render(style.Render(divider))
}

func retainedLabel(retained int) string {
	if retained == 1 {
		return "kept 1 message"
	}

	return fmt.Sprintf("kept %d messages", retained)
}

func tokensLabel(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000)
	}

	return fmt.Sprintf("%d", tokens)
}
