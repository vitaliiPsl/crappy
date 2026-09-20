package transcript

import "strings"

const (
	toolDetailLimit    = 120
	toolDetailEllipsis = "..."

	toolArgumentDescription = "description"
	toolArgumentCommand     = "command"
	toolArgumentPath        = "path"
	toolArgumentURL         = "url"
	toolArgumentQuery       = "query"
	toolArgumentText        = "text"
	toolArgumentID          = "id"
)

var primaryToolArguments = []string{
	toolArgumentCommand,
	toolArgumentPath,
	toolArgumentURL,
	toolArgumentQuery,
	toolArgumentText,
	toolArgumentID,
}

func renderTool(tool toolUse, width int) string {
	content := toolNameStyle.Render(tool.call.Name)
	if primary := toolPrimaryArgument(tool.call.Arguments); primary != "" {
		content += "  " + mutedStyle.Render(primary)
	}

	if description := toolStringArgument(tool.call.Arguments, toolArgumentDescription); description != "" {
		content += "\n" + mutedStyle.Render(description)
	}

	style := toolPendingStyle
	if tool.result != nil {
		if tool.result.Error == "" {
			style = toolSuccessStyle
		} else {
			style = toolErrorStyle
			content += "\n" + mutedStyle.Render(compactToolDetail(tool.result.Error))
		}
	}

	return style.Width(max(width, 1)).Render(content)
}

func toolPrimaryArgument(arguments map[string]any) string {
	for _, name := range primaryToolArguments {
		if value := toolStringArgument(arguments, name); value != "" {
			return value
		}
	}

	return ""
}

func toolStringArgument(arguments map[string]any, name string) string {
	value, ok := arguments[name].(string)
	if !ok {
		return ""
	}

	return compactToolDetail(value)
}

func compactToolDetail(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)

	if len(runes) <= toolDetailLimit {
		return value
	}

	return string(runes[:toolDetailLimit]) + toolDetailEllipsis
}
