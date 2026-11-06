package session

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

const queueHeadingHeight = 2

func (m *Model) applyExecution(record execution.Record) {
	selectedID := ""
	if entries := m.queueEntries(); m.queueSelected < len(entries) {
		selectedID = entries[m.queueSelected].ID
	}

	index := slices.IndexFunc(m.pending, func(pending execution.Record) bool { return pending.ID == record.ID })

	var previous execution.Status
	if index >= 0 {
		previous = m.pending[index].Status
	}

	switch {
	case record.Status.Terminal():
		if index >= 0 {
			m.pending = slices.Delete(m.pending, index, index+1)
		}
	case index >= 0:
		m.pending[index] = record
	default:
		m.pending = append(m.pending, record)
	}

	if selectedID != "" {
		index := slices.IndexFunc(m.queueEntries(), func(pending execution.Record) bool { return pending.ID == selectedID })
		if index >= 0 {
			m.queueSelected = index
		}
	}

	m.ensureQueueVisible()

	if record.Status == execution.Running {
		if m.terminalErr == nil {
			m.notice = notice{}
		}

		m.transcript.Start(record.ID)
	} else if record.Status.Terminal() || record.Status == execution.Interrupted {
		if previous == execution.Running {
			m.transcript.Finish(record.ID)
		}

		if previous != execution.Queued {
			m.notice = executionNotice(record)
		}
	}
}

func (m *Model) queueEntries() []execution.Record {
	if len(m.pending) > 0 && m.pending[0].Status == execution.Running {
		return m.pending[1:]
	}

	return m.pending
}

func (m *Model) queuedCount() int {
	count := 0
	for _, record := range m.pending {
		if record.Status == execution.Queued {
			count++
		}
	}

	return count
}

func (m *Model) showQueue() tea.Cmd {
	m.queueOpen = true
	m.composer.Blur()
	m.ensureQueueVisible()

	return nil
}

func (m *Model) handleQueueKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.queueOpen = false

		return m.composer.Focus()
	case "up", "k":
		m.moveQueue(-1)
	case "down", "j":
		m.moveQueue(1)
	case "pgup", "ctrl+u":
		m.moveQueue(-m.visibleQueueRows())
	case "pgdown", "ctrl+d":
		m.moveQueue(m.visibleQueueRows())
	case "d":
		if entries := m.queueEntries(); m.terminalErr == nil && m.queueSelected < len(entries) {
			return m.cancelExecution(entries[m.queueSelected].ID)
		}
	}

	return nil
}

func (m *Model) moveQueue(delta int) {
	m.queueSelected += delta
	m.ensureQueueVisible()
}

func (m *Model) ensureQueueVisible() {
	m.queueSelected = max(0, min(m.queueSelected, len(m.queueEntries())-1))
	rows := m.visibleQueueRows()

	m.queueOffset = min(m.queueOffset, max(0, len(m.queueEntries())-rows))

	m.queueOffset = min(m.queueOffset, m.queueSelected)
	if m.queueSelected >= m.queueOffset+rows {
		m.queueOffset = m.queueSelected - rows + 1
	}
}

func (m *Model) visibleQueueRows() int {
	return max(1, m.transcriptHeight(m.headerView(), m.statusView(), m.shortcutsView())-queueHeadingHeight)
}

func (m *Model) queueView(height int) string {
	m.ensureQueueVisible()

	lines := []string{queueHeadingStyle.Render("Pending work"), ""}
	entries := m.queueEntries()

	positionWidth := len(fmt.Sprint(len(entries)))
	if len(entries) > 0 && entries[0].Status == execution.Interrupted {
		positionWidth = len(interruptedLabel)
	}

	if len(entries) == 0 {
		lines = append(lines, queueTextStyle.Render("No pending work"))
	}

	end := min(len(entries), m.queueOffset+m.visibleQueueRows())
	for i := m.queueOffset; i < end; i++ {
		marker := "  "

		style := queueTextStyle
		if i == m.queueSelected {
			marker = "› "
			style = queueSelectedStyle
		}

		position := fmt.Sprint(i + 1)
		if entries[i].Status == execution.Interrupted {
			position = interruptedLabel
		} else if len(entries) > 0 && entries[0].Status == execution.Interrupted {
			position = fmt.Sprint(i)
		}

		label := fmt.Sprintf("%s%-*s  %s", marker, positionWidth, position, executionLabel(entries[i]))
		lines = append(lines, style.Render(ansi.Truncate(label, m.contentWidth(), "…")))
	}

	return lipgloss.NewStyle().Width(m.contentWidth()).Height(height).MaxHeight(height).Render(strings.Join(lines, "\n"))
}

func (m *Model) queueHints() string {
	values := []hints.Hint{{Key: "↑/↓", Label: "Select"}}
	if len(m.queueEntries()) > 0 && m.terminalErr == nil {
		values = append(values, hints.Hint{Key: "d", Label: "Cancel"})
	}

	values = append(values, hints.Hint{Key: "Esc", Label: "Back"})

	return hints.Render(m.contentWidth(), values...)
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
