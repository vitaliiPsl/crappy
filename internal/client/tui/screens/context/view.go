package contextview

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/internal/client/tui/components/hints"
)

type itemKind uint8

const (
	instructionsItem itemKind = iota
	instructionItem
	conversationItem
	toolsItem
	messageItem
	toolItem
)

type item struct {
	kind   itemKind
	index  int
	label  string
	tokens int
}

func (m *Model) render() string {
	body := strings.Split(m.bodyView(), "\n")
	if len(body) > m.bodyHeight() {
		body = body[:m.bodyHeight()]
	}

	area := lipgloss.NewStyle().Height(m.bodyHeight()).Render(strings.Join(body, "\n"))
	content := strings.Join([]string{m.headerView(), "", area, "", m.footerView()}, "\n")

	lines := strings.Split(pageStyle.Render(content), "\n")
	if len(lines) > max(m.height, 1) {
		lines = lines[:max(m.height, 1)]
	}

	for index, line := range lines {
		lines[index] = ansi.Truncate(line, max(m.width, 1), "")
	}

	return strings.Join(lines, "\n")
}

func (m *Model) headerView() string {
	title, tokens := "Context", m.data.Tokens()
	switch m.mode {
	case messagesView:
		title, tokens = "Context / Conversation", m.data.MessageTokens()
	case toolsView:
		title, tokens = "Context / Tools", m.data.ToolTokens()
	case detailView:
		title, tokens = "Context / "+m.detailTitle, m.detailTokens
	}

	estimate := ""
	if !m.loading && m.err == nil {
		estimate = "≈" + formatTokens(tokens) + " tokens"
	}

	return strings.Join([]string{
		titleStyle.Render(align(title, estimate, m.contentWidth())),
		subtleStyle.Render(ansi.Truncate(ansi.Strip(m.data.Workdir), m.contentWidth(), "…")),
		subtleStyle.Render(ansi.Truncate("Current snapshot · before the next message and automatic compaction", m.contentWidth(), "…")),
	}, "\n")
}

func (m *Model) bodyView() string {
	if m.loading {
		return subtleStyle.Render("Resolving context…")
	}

	if m.err != nil {
		return errorStyle.Render(ansi.Hardwrap(ansi.Strip(m.err.Error()), m.contentWidth(), false))
	}

	if m.mode == detailView {
		return m.viewport.View()
	}

	items := m.items()
	if len(items) == 0 {
		return subtleStyle.Render("Nothing in this section.")
	}

	end := min(m.offset+m.bodyHeight(), len(items))

	lines := make([]string, 0, end-m.offset)
	for index := m.offset; index < end; index++ {
		current := items[index]

		marker, style := "  ", textStyle
		if index == m.selected {
			marker, style = "› ", selectedStyle
		}

		line := align(marker+current.label, "≈"+formatTokens(current.tokens), m.contentWidth())
		lines = append(lines, style.Render(line))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) footerView() string {
	values := []hints.Hint{{Key: "↑/↓", Label: "Select"}, {Key: "Enter", Label: "Inspect"}}
	if m.mode == detailView {
		values = []hints.Hint{{Key: "↑/↓", Label: "Scroll"}, {Key: "PgUp/PgDn", Label: "Page"}}
	}

	values = append(values, hints.Hint{Key: "r", Label: "Refresh"}, hints.Hint{Key: "Esc", Label: "Back"})

	return hints.Render(m.contentWidth(), values...)
}

func (m *Model) bodyHeight() int {
	return max(m.height-lipgloss.Height(m.headerView())-lipgloss.Height(m.footerView())-2, 1)
}

func (m *Model) contentWidth() int {
	return max(m.width-pageStyle.GetHorizontalFrameSize(), 1)
}

func (m *Model) items() []item {
	switch m.mode {
	case overviewView:
		items := []item{{kind: instructionsItem, label: "Instructions", tokens: m.data.InstructionTokens()}}
		for index, instruction := range m.data.Instructions {
			items = append(items, item{kind: instructionItem, index: index, label: "  " + cmp.Or(instruction.Source, "Core"), tokens: instruction.Tokens})
		}

		return append(items,
			item{kind: conversationItem, label: "Conversation · " + countLabel(len(m.data.Messages), "message"), tokens: m.data.MessageTokens()},
			item{kind: toolsItem, label: "Tools · " + countLabel(len(m.data.Tools), "definition"), tokens: m.data.ToolTokens()},
		)
	case messagesView:
		items := make([]item, 0, len(m.data.Messages))
		for index, message := range m.data.Messages {
			label := fmt.Sprintf("%d · %s · %s", index+1, message.Role, messagePreview(message.Message))
			items = append(items, item{kind: messageItem, index: index, label: label, tokens: message.Tokens})
		}

		return items
	case toolsView:
		items := make([]item, 0, len(m.data.Tools))
		for index, tool := range m.data.Tools {
			items = append(items, item{kind: toolItem, index: index, label: tool.Name, tokens: tool.Tokens})
		}

		return items
	default:
		return nil
	}
}

func (m *Model) inspect() {
	items := m.items()
	if m.selected >= len(items) {
		return
	}

	selected := items[m.selected]
	switch selected.kind {
	case instructionsItem:
		sections := make([]string, 0, len(m.data.Instructions))
		for _, instruction := range m.data.Instructions {
			sections = append(sections, instruction.Text)
		}

		m.showDetail("Instructions", strings.Join(sections, "\n\n"), selected.tokens)
	case instructionItem:
		instruction := m.data.Instructions[selected.index]
		m.showDetail(cmp.Or(instruction.Source, "Core"), instruction.Text, selected.tokens)
	case conversationItem:
		m.open(messagesView)
	case toolsItem:
		m.open(toolsView)
	case messageItem:
		message := m.data.Messages[selected.index]
		m.showDetail(fmt.Sprintf("Message %d · %s", selected.index+1, message.Role), messageDetails(message.Message), selected.tokens)
	case toolItem:
		tool := m.data.Tools[selected.index]
		m.showDetail(tool.Name, tool.Description+"\n\n"+prettyJSON(tool.Schema), selected.tokens)
	}
}

func align(left, right string, width int) string {
	right = ansi.Truncate(ansi.Strip(right), width, "…")
	available := max(width-ansi.StringWidth(right)-2, 0)
	left = ansi.Truncate(ansi.Strip(left), available, "…")

	return left + strings.Repeat(" ", max(width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)) + right
}

func formatTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err.Error()
	}

	return string(data)
}

func messagePreview(message kit.Message) string {
	if text := kit.ContentsText(message.Content); text != "" {
		return strings.Join(strings.Fields(text), " ")
	}

	if calls := message.ToolCalls(); len(calls) > 0 {
		return "call " + calls[0].Name
	}

	if results := message.ToolResults(); len(results) > 0 {
		return "result " + results[0].Call.Name
	}

	return "[non-text content]"
}

func messageDetails(message kit.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, content := range message.Content {
		switch {
		case content.Text != nil:
			parts = append(parts, content.Text.Text)
		case content.Thinking != nil:
			parts = append(parts, "Thinking\n\n"+content.Thinking.Text)
		default:
			parts = append(parts, prettyJSON(content))
		}
	}

	return strings.Join(parts, "\n\n")
}
