package session

import (
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const titleLimit = 60

func inputTitle(input kit.Message) string {
	text := input.TextContent()
	if text == nil {
		return ""
	}

	title := []rune(strings.Join(strings.Fields(text.Text), " "))
	if len(title) <= titleLimit {
		return string(title)
	}

	return strings.TrimSpace(string(title[:titleLimit])) + "…"
}
