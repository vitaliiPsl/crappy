package transcript

import (
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
)

type executionStatus int

const (
	executionRunning executionStatus = iota
	executionCheckpointing
	executionFinished
)

type execution struct {
	id     string
	items  []item
	tools  map[string]*toolUse
	stream stream
	status executionStatus

	rendered string
	width    int
}

type item struct {
	role       kit.Role
	content    []contentItem
	checkpoint *checkpoint.Checkpoint
}

type contentItem struct {
	content kit.Content
	tool    *toolUse
}

type toolUse struct {
	call   kit.ToolCall
	result *kit.ToolResult
}

func newExecution(id string) *execution {
	return &execution{
		id:     id,
		tools:  make(map[string]*toolUse),
		stream: newStream(),
	}
}

func (e *execution) append(message kit.Message) {
	if message.Role == kit.RoleModel {
		e.stream.reset()
	}

	if value, ok := e.newItem(message); ok {
		e.items = append(e.items, value)
	}
}

func (e *execution) apply(event kit.AgentEvent) {
	e.stream.apply(event)
}

func (e *execution) startCheckpoint() {
	e.status = executionCheckpointing
}

func (e *execution) appendCheckpoint(generated checkpoint.Checkpoint) {
	if e.status == executionCheckpointing {
		e.status = executionRunning
	}

	e.items = append(e.items, item{checkpoint: &generated})
}

func (e *execution) finish() {
	e.stream.reset()
	e.status = executionFinished
}

func (e *execution) newItem(message kit.Message) (item, bool) {
	value := item{
		role:    message.Role,
		content: make([]contentItem, 0, len(message.Content)),
	}

	for _, content := range message.Content {
		switch content.Type {
		case kit.ContentTypeSummary:
			continue
		case kit.ContentTypeToolCall:
			if content.ToolCall == nil {
				continue
			}

			tool := &toolUse{call: *content.ToolCall}
			value.content = append(value.content, contentItem{tool: tool})

			if tool.call.ID != "" {
				e.tools[tool.call.ID] = tool
			}
		case kit.ContentTypeToolResult:
			if content.ToolResult == nil {
				continue
			}

			result := *content.ToolResult
			if tool := e.tools[result.Call.ID]; result.Call.ID != "" && tool != nil {
				tool.result = &result

				continue
			}

			value.content = append(value.content, contentItem{
				tool: &toolUse{call: result.Call, result: &result},
			})
		default:
			value.content = append(value.content, contentItem{content: content})
		}
	}

	return value, len(value.content) > 0
}

func (e *execution) render(width int) string {
	if e.status == executionFinished && e.width == width {
		return e.rendered
	}

	sections := make([]string, 0, len(e.items)+1)
	for _, value := range e.items {
		if rendered := renderMessage(value, width); rendered != "" {
			sections = append(sections, rendered)
		}
	}

	if value, ok := pendingItem(e.stream.content); ok {
		if rendered := renderMessage(value, width); rendered != "" {
			sections = append(sections, rendered)
		}
	}

	if e.status == executionCheckpointing {
		sections = append(sections, renderPendingCheckpoint(width))
	}

	rendered := strings.Join(sections, sectionSeparator)

	if e.status == executionFinished {
		e.rendered = rendered
		e.width = width
	}

	return rendered
}

func pendingItem(content []kit.Content) (item, bool) {
	value := item{
		role:    kit.RoleModel,
		content: make([]contentItem, 0, len(content)),
	}

	for _, current := range content {
		switch current.Type {
		case kit.ContentTypeSummary, kit.ContentTypeToolResult:
			continue
		case kit.ContentTypeToolCall:
			if current.ToolCall == nil {
				continue
			}

			value.content = append(value.content, contentItem{
				tool: &toolUse{call: *current.ToolCall},
			})
		default:
			value.content = append(value.content, contentItem{content: current})
		}
	}

	return value, len(value.content) > 0
}
