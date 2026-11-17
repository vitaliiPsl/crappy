package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestToolPreservesRichAndStructuredErrorOutput(t *testing.T) {
	t.Parallel()

	result := &mcpsdk.CallToolResult{
		IsError: true, StructuredContent: map[string]any{"retryable": false},
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: "The requested record is unavailable."},
			&mcpsdk.ImageContent{MIMEType: "image/png", Data: []byte("image")},
			&mcpsdk.AudioContent{MIMEType: "audio/wav", Data: []byte("audio")},
			&mcpsdk.ResourceLink{URI: "test://record", Name: "record", Title: "Record"},
			&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{URI: "test://context", Text: "context"}},
		},
	}
	server := newTestServer()
	definition := &mcpsdk.Tool{Name: "details", Description: "Read details", InputSchema: map[string]any{"type": "object"}}
	server.AddTool(definition, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return result, nil
	})
	connections := connected(t, ServerConfig{Name: "records", Tools: []string{"details"}, URL: serveHTTP(t, server).URL})

	remote := connections.Servers()[0].Tools[0]

	output, err := remote.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", remote.Definition().Name, nil))
	if err == nil || !strings.Contains(err.Error(), "record is unavailable") || len(output.Content) != 5 || output.Structured == nil {
		t.Fatalf("remote error lost its output: %+v, %v", output, err)
	}

	if string(output.Content[1].Image.Data) != "image" || string(output.Content[2].Audio.Data) != "audio" ||
		output.Content[3].Resource.URI != "test://record" || output.Content[4].Resource.Text != "context" {
		t.Fatalf("rich content changed: %+v", output.Content)
	}
}

func TestToolsKeepExistingTimeoutAndOutputCap(t *testing.T) {
	t.Parallel()

	connections := connected(t, ServerConfig{Name: "test", URL: serveHTTP(t, newTestServer()).URL})

	limits := tools.DefaultConfig()
	limits.Timeout = 100 * time.Millisecond
	limits.MaxOutputBytes = 1024

	var stored string

	capOutput := tools.OutputCap(limits, func(_ context.Context, _ string, data []byte) (string, error) {
		stored = string(data)

		return "/tmp/mcp-output.txt", nil
	})
	for _, remote := range connections.Servers()[0].Tools {
		wrapped, err := tools.Timeout(limits)(agent.BuildContext{}, remote)
		if err != nil {
			t.Fatal(err)
		}

		wrapped, err = capOutput(agent.BuildContext{}, wrapped)
		if err != nil {
			t.Fatal(err)
		}

		name := remote.Definition().Name
		if !strings.HasSuffix(name, "__slow") && !strings.HasSuffix(name, "__large") {
			continue
		}

		output, err := wrapped.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", name, nil))
		if strings.HasSuffix(name, "__slow") {
			if err == nil || !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("remote timeout = %v", err)
			}

			continue
		}

		text := kit.ContentsText(output.Content)
		if err != nil || len(text) > limits.MaxOutputBytes || !strings.Contains(text, "full output:") || len(stored) <= limits.MaxOutputBytes {
			t.Fatalf("remote output capping = %q, %v", text, err)
		}
	}
}

func TestToolRespectsCallerCancellation(t *testing.T) {
	t.Parallel()

	connections := connected(t, ServerConfig{Name: "test", Tools: []string{"slow"}, URL: serveHTTP(t, newTestServer()).URL})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	remote := connections.Servers()[0].Tools[0]

	_, err := remote.Execute(kit.NewRunContext(ctx), kit.NewToolCall("call", remote.Definition().Name, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call = %v", err)
	}
}
