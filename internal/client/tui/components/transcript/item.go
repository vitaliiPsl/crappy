package transcript

import "github.com/vitaliiPsl/crappy-adk/kit"

type item struct {
	role    kit.Role
	content []contentItem
}

type contentItem struct {
	content kit.Content
	tool    *toolUse
}

type toolUse struct {
	call   kit.ToolCall
	result *kit.ToolResult
}

func newMessageItem(message kit.Message, tools map[string]*toolUse) (item, bool) {
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
				tools[tool.call.ID] = tool
			}
		case kit.ContentTypeToolResult:
			if content.ToolResult == nil {
				continue
			}

			result := *content.ToolResult
			if tool := tools[result.Call.ID]; result.Call.ID != "" && tool != nil {
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
