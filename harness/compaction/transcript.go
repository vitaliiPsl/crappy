package compaction

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func flatten(messages []kit.Message) string {
	var output strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&output, "%s:\n", message.Role)

		for _, content := range message.Content {
			writeContent(&output, content)
		}

		output.WriteByte('\n')
	}

	return strings.TrimRight(output.String(), "\n")
}

func writeContent(output *strings.Builder, content kit.Content) {
	switch content.Type {
	case kit.ContentTypeToolCall:
		if content.ToolCall == nil {
			return
		}

		arguments, _ := json.Marshal(content.ToolCall.Arguments)
		fmt.Fprintf(output, "[Tool call: %s(%s)]\n", content.ToolCall.Name, arguments)
	case kit.ContentTypeToolResult:
		writeToolResult(output, content.ToolResult)
	case kit.ContentTypeText,
		kit.ContentTypeImage,
		kit.ContentTypeAudio,
		kit.ContentTypeResource:
		if text, ok := kit.ContentText(content); ok {
			fmt.Fprintln(output, text)
		}
	}
}

func writeToolResult(output *strings.Builder, result *kit.ToolResult) {
	if result == nil {
		return
	}

	if result.Error != "" {
		fmt.Fprintf(output, "[Tool error from %s: %s]\n", result.Call.Name, result.Error)

		return
	}

	fmt.Fprintf(output, "[Tool result from %s]\n", result.Call.Name)

	for _, content := range result.Output.Content {
		writeContent(output, content)
	}

	if result.Output.Structured == nil {
		return
	}

	data, err := json.Marshal(result.Output.Structured)
	if err != nil {
		return
	}

	fmt.Fprintf(output, "[Structured output]\n%s\n", data)
}
