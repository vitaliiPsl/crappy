package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/vitaliipsl/crappy/harness/agent"
)

func TestContributorReadsServersAndReportsFailures(t *testing.T) {
	t.Parallel()

	connections := connected(t,
		ServerConfig{Name: "work", Tools: []string{"echo"}, URL: serveHTTP(t, newTestServer()).URL},
		ServerConfig{Name: "offline", Command: "/does-not-exist/crappy-mcp-server"},
	)

	contributor := NewContributor(connections)
	for range 2 {
		contribution, err := contributor.Contribute(t.Context(), agent.BuildContext{})
		if err != nil {
			t.Fatal(err)
		}

		if len(contribution.Tools) != 1 || contribution.Tools[0].Definition().Name != "mcp__work__echo" {
			t.Fatalf("contributed tools = %+v", contribution.Tools)
		}

		instructions := strings.Join(contribution.Instructions, "\n")
		if !strings.Contains(instructions, "Use these tools for testing.") || !strings.Contains(instructions, `MCP server "offline" is unavailable`) {
			t.Fatalf("missing instructions or availability: %s", instructions)
		}

		contribution.Tools[0] = nil
	}

	if err := connections.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	contribution, err := contributor.Contribute(t.Context(), agent.BuildContext{})
	if err != nil || len(contribution.Tools) != 0 {
		t.Fatalf("closed connections still contribute tools: %+v, %v", contribution.Tools, err)
	}
}
