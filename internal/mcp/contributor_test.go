package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/discovery"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/internal/tools"
)

func TestContributorReadsServersAndReportsFailures(t *testing.T) {
	t.Parallel()

	connections := connected(t,
		ServerConfig{Name: "work", Tools: []string{"echo"}, URL: serveHTTP(t, newTestServer()).URL},
		ServerConfig{Name: "offline", Command: "/does-not-exist/crappy-mcp-server"},
		ServerConfig{Name: "locked", URL: serveProtected(t).URL},
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
		if !strings.Contains(instructions, "Use these tools for testing.") || !strings.Contains(instructions, `MCP server "offline" is unavailable`) ||
			!strings.Contains(instructions, "`crappy mcp login locked`") {
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

func TestContributorDiscoversAndExecutesPreparedMCPTools(t *testing.T) {
	t.Parallel()

	connections := connected(t, ServerConfig{Name: "work", URL: serveHTTP(t, newTestServer()).URL})
	turn := func(calls ...kit.ToolCall) kittest.ModelResult {
		var content []kit.Content
		for _, call := range calls {
			content = append(content, kit.NewToolCallContent(call))
		}

		return kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(content...), FinishReason: kit.FinishReasonToolCall,
		}}
	}
	model := kittest.NewModel(t,
		turn(kit.NewToolCall("search", discovery.SearchToolName, map[string]any{"query": "echo"})),
		turn(kit.NewToolCall("echo", "mcp__work__echo", map[string]any{"text": "hello"})),
		turn(kit.NewToolCall("load", discovery.LoadToolName, map[string]any{
			"names": []any{"mcp__work__large", "mcp__work__slow"},
		})),
		turn(kit.NewToolCall("large", "mcp__work__large", nil), kit.NewToolCall("slow", "mcp__work__slow", nil)),
		kittest.ModelResult{Response: kit.ModelResponse{
			Message: kit.NewModelMessage(kit.NewTextContent("done")), FinishReason: kit.FinishReasonStop,
		}},
	)
	limits := tools.DefaultConfig()
	limits.Timeout = 100 * time.Millisecond
	limits.MaxOutputBytes = 1024

	var stored []byte

	capOutput := tools.OutputCap(limits, func(_ context.Context, _ string, data []byte) (string, error) {
		stored = data

		return "/tmp/discovered-output.txt", nil
	})
	factory := agent.New(kit.AgentConfig{}, contributorModelResolver{model},
		agent.WithToolSetFactory(func(registry kit.ToolSet) (kit.ToolSet, error) {
			return discovery.NewToolSet(registry, func(tool kit.Tool) bool {
				return strings.HasPrefix(tool.Definition().Name, ToolPrefix)
			})
		}),
		agent.WithContributors(NewContributor(connections)),
		agent.WithToolMiddleware(capOutput, tools.Timeout(limits)),
	)
	store := entry.NewMemoryStore()
	broker := event.NewBroker("session", store)
	t.Cleanup(broker.Close)
	build := agent.BuildRequest{
		SessionID: "session", ExecutionID: "execution",
		History: agent.NewHistory("session", "execution", store, broker),
	}

	built, err := factory.Build(t.Context(), build)
	if err != nil {
		t.Fatal(err)
	}

	result, err := built.Run(t.Context(), kit.NewUserMessage(kit.NewTextContent("use the connected tools")))
	if err != nil {
		t.Fatal(err)
	}

	if len(model.CallAt(0).Tools) != 2 || len(model.CallAt(1).Tools) != 3 || len(model.CallAt(3).Tools) != 5 {
		t.Fatal("model requests did not expose tools on demand")
	}

	for _, exposed := range model.CallAt(1).Tools {
		properties := exposed.Definition().Schema["properties"].(map[string]any)
		if _, found := properties["timeout_seconds"]; !found {
			t.Fatalf("tool %q lost its prepared schema", exposed.Definition().Name)
		}
	}

	var capped, timedOut bool
	for _, message := range result.Messages {
		for _, toolResult := range message.ToolResults() {
			if toolResult.Call.Name == "mcp__work__large" {
				text := kit.ContentsText(toolResult.Output.Content)
				capped = strings.Contains(text, "full output:") && len(text) <= limits.MaxOutputBytes
			}

			if toolResult.Call.Name == "mcp__work__slow" {
				timedOut = strings.Contains(toolResult.Error, "timed out")
			}
		}
	}

	if !capped || !timedOut || len(stored) <= limits.MaxOutputBytes {
		t.Fatalf("prepared execution: capped=%v timedOut=%v stored=%d", capped, timedOut, len(stored))
	}

	fresh, err := factory.Build(t.Context(), build)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := fresh.Context(t.Context())
	if err != nil || len(preview.Tools) != 2 {
		t.Fatalf("selection leaked across agent builds: %+v, %v", preview, err)
	}
}

type contributorModelResolver struct {
	model kit.Model
}

func (r contributorModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
