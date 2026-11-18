package session

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func (m *Model) applyExecution(record execution.Record) {
	if record.Status.Terminal() || record.Status == execution.Interrupted {
		m.prompt.Interrupt(record.ID)

		if !m.prompt.Active() {
			m.composer.Focus()
		}
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

	m.queue.SetEntries(m.queueEntries())

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
	m.queue.Open()
	m.composer.Blur()

	return nil
}
