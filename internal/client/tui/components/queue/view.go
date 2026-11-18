package queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

const interruptedLabel = "Interrupted"

func (m Model) View() string {
	lines := []string{headingStyle.Render("Pending work"), ""}

	interrupted := len(m.entries) > 0 && m.entries[0].Status == execution.Interrupted

	positionWidth := len(fmt.Sprint(len(m.entries)))
	if interrupted {
		positionWidth = len(interruptedLabel)
	}

	if len(m.entries) == 0 {
		lines = append(lines, textStyle.Render("No pending work"))
	}

	end := min(len(m.entries), m.offset+m.rows())
	for i := m.offset; i < end; i++ {
		marker, style := "  ", textStyle
		if i == m.selected {
			marker, style = "› ", selectedStyle
		}

		position := fmt.Sprint(i + 1)
		if m.entries[i].Status == execution.Interrupted {
			position = interruptedLabel
		} else if interrupted {
			position = fmt.Sprint(i)
		}

		label := fmt.Sprintf("%s%-*s  %s", marker, positionWidth, position, executionLabel(m.entries[i]))
		lines = append(lines, style.Render(ansi.Truncate(label, m.width, "…")))
	}

	return lipgloss.NewStyle().Width(m.width).Height(m.height).MaxHeight(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) Shortcuts(cancellable bool) string {
	values := []hints.Hint{{Key: "↑/↓", Label: "Select"}}
	if len(m.entries) > 0 && cancellable {
		values = append(values, hints.Hint{Key: "d", Label: "Cancel"})
	}

	values = append(values, hints.Hint{Key: "Esc", Label: "Back"})

	return hints.Render(m.width, values...)
}

func executionLabel(record execution.Record) string {
	label := record.Executor
	switch record.Executor {
	case compactionexec.ExecutorName:
		label = "/compact"
	case agent.ExecutorName:
		var message kit.Message
		if err := json.Unmarshal(record.Input, &message); err == nil {
			label = kit.ContentsText(message.Content)
		}
	}

	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, ansi.Strip(label))

	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return "Message"
	}

	return label
}
