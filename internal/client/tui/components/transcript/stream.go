package transcript

import "github.com/vitaliiPsl/crappy-adk/kit"

type stream struct {
	content []kit.Content
	active  int
}

func newStream() stream {
	return stream{active: -1}
}

func (s *stream) apply(event kit.AgentEvent) {
	switch event.Type {
	case kit.EventContentStarted:
		s.start(event.Content)
	case kit.EventContentDelta:
		s.appendDelta(event.Content)
	case kit.EventContentDone:
		if event.ToolResult == nil {
			s.finish(event.Content)
		}
	}
}

func (s *stream) reset() {
	s.content = nil
	s.active = -1
}

func (s *stream) start(content *kit.Content) {
	if content == nil {
		return
	}

	s.content = append(s.content, cloneContent(*content))
	s.active = len(s.content) - 1
}

func (s *stream) appendDelta(content *kit.Content) {
	if content == nil {
		return
	}

	if s.active < 0 ||
		s.active >= len(s.content) ||
		s.content[s.active].Type != content.Type {
		s.content = append(s.content, kit.Content{Type: content.Type})
		s.active = len(s.content) - 1
	}

	mergeContent(&s.content[s.active], *content)
}

func (s *stream) finish(content *kit.Content) {
	if content == nil {
		return
	}

	completed := cloneContent(*content)
	if s.active >= 0 &&
		s.active < len(s.content) &&
		s.content[s.active].Type == content.Type {
		s.content[s.active] = completed
	} else {
		s.content = append(s.content, completed)
	}

	s.active = -1
}

func cloneContent(content kit.Content) kit.Content {
	if content.Text != nil {
		value := *content.Text
		content.Text = &value
	}

	if content.Thinking != nil {
		value := *content.Thinking
		content.Thinking = &value
	}

	return content
}

func mergeContent(target *kit.Content, delta kit.Content) {
	switch delta.Type {
	case kit.ContentTypeText:
		if delta.Text == nil {
			return
		}

		if target.Text == nil {
			target.Text = &kit.Text{}
		}

		target.Text.Text += delta.Text.Text
	case kit.ContentTypeThinking:
		if delta.Thinking == nil {
			return
		}

		if target.Thinking == nil {
			target.Thinking = &kit.Thinking{}
		}

		target.Thinking.Text += delta.Thinking.Text
	default:
		*target = cloneContent(delta)
	}
}
