package report

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/service"
)

func MCP(servers []service.MCPServer) string {
	if len(servers) == 0 {
		return detailStyle.Render("No MCP servers configured. Add them under mcp.servers in settings.")
	}

	nameWidth, statusWidth := 0, 0
	for _, server := range servers {
		status, _, _ := mcpStatus(server)
		nameWidth = max(nameWidth, ansi.StringWidth(ansi.Strip(server.Name)))
		statusWidth = max(statusWidth, len(status))
	}

	lines := []string{headingStyle.Render("MCP servers"), ""}
	for _, server := range servers {
		name := ansi.Strip(server.Name)
		status, detail, style := mcpStatus(server)

		line := valueStyle.Render(name+strings.Repeat(" ", nameWidth-ansi.StringWidth(name))) + "  " +
			style.Render(fmt.Sprintf("%-*s", statusWidth, status))
		if detail != "" {
			line += "  " + countStyle.Render(detail)
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func mcpStatus(server service.MCPServer) (string, string, lipgloss.Style) {
	switch server.Status {
	case service.MCPServerConnected:
		return "connected", Count(server.Tools, "tool"), connectedStyle
	case service.MCPServerAuthorizationRequired:
		return "needs login", "run: crappy mcp login " + ansi.Strip(server.Name), warningStyle
	case service.MCPServerUnavailable:
		return "unavailable", strings.Join(strings.Fields(ansi.Strip(server.Error)), " "), failureStyle
	default:
		return "connecting", "", countStyle
	}
}
