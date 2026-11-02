package transcript

import "github.com/charmbracelet/x/ansi"

type commandOutput struct {
	name    string
	content string
}

func newCommandOutput(name, content string) *commandOutput {
	return &commandOutput{name: name, content: content}
}

func (c *commandOutput) render(width int) string {
	contentWidth := max(width-assistantStyle.GetHorizontalFrameSize(), 1)
	title := commandStyle.Render(ansi.Hardwrap("/"+ansi.Strip(c.name), contentWidth, false))
	content := ansi.Hardwrap(c.content, contentWidth, false)

	return assistantStyle.Width(max(width, 1)).Render(title + "\n" + content)
}
