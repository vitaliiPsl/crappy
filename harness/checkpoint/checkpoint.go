package checkpoint

import (
	"fmt"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const EntryType = "checkpoint"

const messagePreamble = "The conversation before this point was compacted into the following checkpoint. " +
	"The messages after it are kept verbatim."

type Summary struct {
	Objective string   `json:"objective" jsonschema:"What the user wants to achieve, including their requirements, constraints and preferences."`
	Progress  []string `json:"progress" jsonschema:"Work that is done, in progress or blocked. Start each item with its status: done, active or blocked."`
	Context   []string `json:"context" jsonschema:"Facts needed to continue: decisions and their reasons, findings, and exact file paths, identifiers, commands, URLs and error messages."`
	Next      []string `json:"next" jsonschema:"Ordered next actions, starting with the immediate one."`
}

type Checkpoint struct {
	Summary

	Retained int       `json:"retained"`
	Usage    kit.Usage `json:"usage"`
}

func (c Checkpoint) Message() kit.Message {
	var text strings.Builder

	text.WriteString(messagePreamble)
	text.WriteString("\n\n<checkpoint>\n")
	fmt.Fprintf(&text, "## Objective\n%s\n", c.Objective)
	writeSection(&text, "Progress", c.Progress)
	writeSection(&text, "Context", c.Context)
	writeSection(&text, "Next", c.Next)
	text.WriteString("</checkpoint>")

	return kit.NewUserMessage(kit.NewTextContent(text.String()))
}

func writeSection(text *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}

	fmt.Fprintf(text, "\n## %s\n", title)

	for _, item := range items {
		fmt.Fprintf(text, "- %s\n", item)
	}
}
