package session

func (m *Model) resize() {
	m.composer.SetWidth(m.width)
	m.transcript.SetSize(m.width, max(m.height-m.composer.Height()-2, 1))
}

func (m *Model) status() string {
	if m.err != nil {
		return "Error: " + m.err.Error()
	}

	return ""
}
