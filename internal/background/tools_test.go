package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/tools"
)

func TestGetToolCapsTextStructuredOutputAndErrors(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	large := strings.Repeat("long output\n", 1000)

	record, err := manager.Start(t.Context(), "session", "bash", time.Minute, func(context.Context) (kit.ToolOutput, error) {
		return kit.ToolOutput{
			Content:    []kit.Content{kit.NewTextContent(large)},
			Structured: map[string]any{"result": large},
		}, errors.New(large)
	})
	if err != nil {
		t.Fatal(err)
	}

	receiveCompletion(t.Context(), t, manager)

	blobs := make(map[string][]byte)
	capOutput := tools.OutputCap(tools.Config{MaxOutputBytes: 1024}, func(_ context.Context, sessionID string, data []byte) (string, error) {
		if sessionID != "session" {
			t.Errorf("blob session = %s", sessionID)
		}

		path := fmt.Sprintf("/output/%d", len(blobs))
		blobs[path] = data

		return path, nil
	})

	get, err := capOutput(agent.BuildContext{Request: agent.BuildRequest{SessionID: "session"}}, findTool(t, NewTools(manager, "session"), "job_get"))
	if err != nil {
		t.Fatal(err)
	}

	output, err := get.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("get", "job_get", map[string]any{"id": record.ID}))
	if err != nil {
		t.Fatal(err)
	}

	if len(blobs) != 3 || len(output.Content) != 3 || output.Structured != nil {
		t.Fatalf("output escaped capping: blobs=%d, output=%+v", len(blobs), output)
	}

	for _, content := range output.Content {
		text := content.Text.Text
		if len(text) > 1024 || !strings.Contains(text, "full output: /output/") {
			t.Fatalf("unbounded output: %q", text)
		}
	}

	_, err = findTool(t, NewTools(manager, "other"), "job_get").Execute(
		kit.NewRunContext(t.Context()), kit.NewToolCall("get", "job_get", map[string]any{"id": record.ID}),
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("job_get crossed session boundary: %v", err)
	}
}

func findTool(t *testing.T, tools []kit.Tool, name string) kit.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool
		}
	}

	t.Fatalf("tool %s not found", name)

	return nil
}
