package transcript

import "github.com/vitaliiPsl/crappy-adk/kit"

type projection struct {
	items  []item
	tools  map[string]*toolUse
	stream stream
}

func newProjection() projection {
	return projection{
		tools:  make(map[string]*toolUse),
		stream: newStream(),
	}
}

func (p *projection) append(message kit.Message) {
	if message.Role == kit.RoleModel {
		p.stream.reset()
	}

	value, ok := newMessageItem(message, p.tools)
	if ok {
		p.items = append(p.items, value)
	}
}

func (p *projection) apply(event kit.AgentEvent) {
	p.stream.apply(event)
}
