package report

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliipsl/crappy/internal/service"
)

func TestMCPShowsServerStatus(t *testing.T) {
	output := ansi.Strip(MCP([]service.MCPServer{
		{Name: "linear", Status: service.MCPServerConnected, Tools: 70},
		{Name: "github", Status: service.MCPServerAuthorizationRequired},
		{Name: "files", Status: service.MCPServerUnavailable, Error: "exec: \"foo\":\nnot found"},
		{Name: "slow\x1b[2J", Status: service.MCPServerConnecting},
	}))

	text := strings.Join(strings.Fields(output), " ")
	for _, want := range []string{
		"MCP servers", "linear connected 70 tools", "github needs login run: crappy mcp login github",
		`files unavailable exec: "foo": not found`, "slow connecting",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("MCP output omitted %q: %s", want, output)
		}
	}

	if strings.Contains(output, "\x1b[2J") {
		t.Fatalf("unsafe MCP labels: %q", output)
	}

	if empty := ansi.Strip(MCP(nil)); !strings.Contains(empty, "No MCP servers configured") {
		t.Fatalf("empty MCP output = %q", empty)
	}
}
