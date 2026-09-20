package transcript

import (
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func (m *Model) refresh() {
	parts := make([]string, 0, len(m.messages)+1)
	for _, message := range m.messages {
		if rendered := renderMessage(message); rendered != "" {
			parts = append(parts, rendered)
		}
	}

	if rendered := renderMessage(kit.NewModelMessage(m.streaming...)); rendered != "" {
		parts = append(parts, rendered)
	}

	m.viewport.SetContent(strings.Join(parts, "\n\n"))
}

func renderMessage(message kit.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, content := range message.Content {
		if text, ok := kit.ContentText(content); ok && text != "" {
			parts = append(parts, text)

			continue
		}

		switch content.Type {
		case kit.ContentTypeToolCall:
			if content.ToolCall != nil {
				parts = append(parts, "Tool: "+content.ToolCall.Name)
			}
		case kit.ContentTypeToolResult:
			if content.ToolResult != nil {
				parts = append(parts, renderToolResult(*content.ToolResult))
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	if message.Role == kit.RoleUser {
		parts[0] = "You: " + parts[0]
	}

	return strings.Join(parts, "\n")
}

func renderToolResult(result kit.ToolResult) string {
	if result.Error != "" {
		return "Tool error: " + result.Error
	}

	return "Tool finished: " + result.Call.Name
}
