package transcript

import "github.com/vitaliiPsl/crappy-adk/kit"

func (m *Model) Apply(event kit.AgentEvent) {
	switch event.Type {
	case kit.EventContentStarted:
		m.start(event.Content)
	case kit.EventContentDelta:
		m.appendDelta(event.Content)
	case kit.EventContentDone:
		if event.ToolResult == nil {
			m.finish(event.Content)
		}
	}

	m.refresh()
	m.viewport.GotoBottom()
}

func (m *Model) start(content *kit.Content) {
	if content == nil {
		return
	}

	m.streaming = append(m.streaming, *content)
	m.active = len(m.streaming) - 1
}

func (m *Model) appendDelta(content *kit.Content) {
	if content == nil {
		return
	}

	if m.active < 0 || m.active >= len(m.streaming) || m.streaming[m.active].Type != content.Type {
		m.start(&kit.Content{Type: content.Type})
	}

	target := &m.streaming[m.active]
	switch content.Type {
	case kit.ContentTypeText:
		if content.Text == nil {
			return
		}

		if target.Text == nil {
			target.Text = &kit.Text{}
		}

		target.Text.Text += content.Text.Text
	case kit.ContentTypeThinking:
		if content.Thinking == nil {
			return
		}

		if target.Thinking == nil {
			target.Thinking = &kit.Thinking{}
		}

		target.Thinking.Text += content.Thinking.Text
	default:
		*target = *content
	}
}

func (m *Model) finish(content *kit.Content) {
	if content == nil {
		return
	}

	if m.active < 0 || m.active >= len(m.streaming) || m.streaming[m.active].Type != content.Type {
		m.streaming = append(m.streaming, *content)
	} else {
		m.streaming[m.active] = *content
	}

	m.active = -1
}
