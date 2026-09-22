package transcript

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const (
	emptyTitle    = "CRAPPY"
	emptyHeadline = "What are we working on?"
	emptyPadding  = 4

	sectionSeparator = "\n\n"
)

func renderMessage(value item, width int) string {
	switch value.role {
	case kit.RoleUser:
		content := renderContentItems(
			value.content,
			max(width-userStyle.GetHorizontalFrameSize(), 1),
		)
		if content == "" {
			return ""
		}

		return userStyle.Width(max(width, 1)).Render(content)
	default:
		content := renderContentItems(
			value.content,
			max(width-assistantStyle.GetHorizontalFrameSize(), 1),
		)
		if content == "" {
			return ""
		}

		return assistantStyle.Width(max(width, 1)).Render(content)
	}
}

func renderContentItems(content []contentItem, width int) string {
	parts := make([]string, 0, len(content))
	for _, value := range content {
		if rendered := renderContentItem(value, width); rendered != "" {
			parts = append(parts, rendered)
		}
	}

	return strings.Join(parts, "\n")
}

func renderContentItem(value contentItem, width int) string {
	if value.tool != nil {
		return renderTool(*value.tool, width)
	}

	content := value.content
	switch content.Type {
	case kit.ContentTypeText:
		if content.Text != nil {
			return content.Text.Text
		}
	case kit.ContentTypeThinking:
		if content.Thinking != nil && content.Thinking.Text != "" {
			return thinkingStyle.Render(thinkingLabel(content.Thinking.Text))
		}
	case kit.ContentTypeImage:
		if content.Image != nil {
			return mutedStyle.Render(mediaLabel("image", content.Image.MIMEType, content.Image.URI))
		}
	case kit.ContentTypeAudio:
		if content.Audio != nil {
			return mutedStyle.Render(mediaLabel("audio", content.Audio.MIMEType, content.Audio.URI))
		}
	case kit.ContentTypeResource:
		if content.Resource != nil {
			label := content.Resource.Title

			if label == "" {
				label = content.Resource.Name
			}

			if label == "" {
				label = content.Resource.URI
			}

			return mutedStyle.Render(mediaLabel("resource", content.Resource.MIMEType, label))
		}
	}

	return ""
}

func thinkingLabel(text string) string {
	characters := len([]rune(text))
	if characters >= 1000 {
		return fmt.Sprintf("Thinking · %.1fk chars", float64(characters)/1000)
	}

	return fmt.Sprintf("Thinking · %d chars", characters)
}

func renderEmpty(width, height int) string {
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		emptyTitleStyle.Render(emptyTitle),
		"",
		emptyTextStyle.Render(emptyHeadline),
	)

	contentHeight := lipgloss.Height(content)
	paddingTop := max((height-contentHeight-emptyPadding)/2, 0)

	return strings.Repeat("\n", paddingTop) +
		lipgloss.NewStyle().Width(max(width, 1)).Align(lipgloss.Center).Render(content)
}

func mediaLabel(kind, mimeType, detail string) string {
	values := make([]string, 0, 2)
	if mimeType != "" {
		values = append(values, mimeType)
	}

	if detail != "" {
		values = append(values, detail)
	}

	if len(values) == 0 {
		return "[" + kind + "]"
	}

	return "[" + kind + ": " + strings.Join(values, ", ") + "]"
}
