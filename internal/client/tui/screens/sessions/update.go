package sessions

import (
	"slices"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

func (m *Model) applyLoaded(message loadedMsg) {
	if message.target != m {
		return
	}

	m.loading = false
	m.err = message.err

	if message.err != nil {
		return
	}

	m.sessions = withCurrent(message.sessions, m.currentID)
	m.selected = sessionIndex(m.sessions, m.currentID)
	m.ensureVisible()
}

func (m *Model) applyDeleted(message deletedMsg) {
	if message.target != m {
		return
	}

	m.deleting = false
	m.deleteID = ""
	m.err = message.err

	if message.err != nil {
		return
	}

	m.sessions = withoutSession(m.sessions, message.sessionID)
	m.selected = min(m.selected, max(len(m.sessions)-1, 0))
	m.ensureVisible()
}

func withCurrent(sessions []state.Record, currentID string) []state.Record {
	values := slices.Clone(sessions)
	if currentID != "" && !hasSession(values, currentID) {
		values = append([]state.Record{{ID: currentID}}, values...)
	}

	return values
}

func hasSession(sessions []state.Record, sessionID string) bool {
	for _, value := range sessions {
		if value.ID == sessionID {
			return true
		}
	}

	return false
}

func sessionIndex(sessions []state.Record, sessionID string) int {
	for index, value := range sessions {
		if value.ID == sessionID {
			return index
		}
	}

	return 0
}

func withoutSession(sessions []state.Record, sessionID string) []state.Record {
	values := make([]state.Record, 0, len(sessions))

	for _, value := range sessions {
		if value.ID != sessionID {
			values = append(values, value)
		}
	}

	return values
}
