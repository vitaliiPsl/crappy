package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	adktool "github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/harness/agent"
)

type testTool struct {
	definition kit.ToolDefinition
	execute    func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error)
}

func (t testTool) Definition() kit.ToolDefinition {
	return t.definition
}

func (t testTool) Execute(
	run *kit.RunContext,
	call kit.ToolCall,
) (kit.ToolOutput, error) {
	return t.execute(run, call)
}

func TestTimeoutExposesAgentOverride(t *testing.T) {
	t.Parallel()

	config := Config{
		Timeout:    15 * time.Second,
		MaxTimeout: time.Minute,
	}

	wrapped, err := Timeout(config)(agent.BuildContext{}, testTool{
		definition: kit.ToolDefinition{Name: "test"},
	})
	if err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}

	property := timeoutProperty(t, wrapped.Definition())
	if got, want := property["maximum"], int64(60); got != want {
		t.Fatalf("maximum = %#v, want %#v", got, want)
	}

	description, ok := property["description"].(string)
	if !ok || !strings.Contains(description, "default 15") {
		t.Fatalf("description = %#v, want default 15", property["description"])
	}
}

func TestTimeoutUsesRequestedTimeoutAndRemovesArgument(t *testing.T) {
	t.Parallel()

	var received kit.ToolCall

	wrapped, err := Timeout(Config{
		Timeout:    time.Second,
		MaxTimeout: time.Minute,
	})(agent.BuildContext{}, testTool{
		definition: kit.ToolDefinition{Name: "test"},
		execute: func(run *kit.RunContext, call kit.ToolCall) (kit.ToolOutput, error) {
			received = call

			deadline, ok := run.Deadline()
			if !ok {
				t.Fatal("tool context has no deadline")
			}

			if remaining := time.Until(deadline); remaining <= time.Second || remaining > 2*time.Second {
				t.Fatalf("deadline remaining = %v, want about 2s", remaining)
			}

			return kit.ToolOutput{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}

	call := kit.NewToolCall("call", "test", map[string]any{
		"value":         "kept",
		timeoutArgument: float64(2),
	})
	if _, err := wrapped.Execute(kit.NewRunContext(context.Background()), call); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := received.Arguments["value"], "kept"; got != want {
		t.Fatalf("value = %#v, want %#v", got, want)
	}

	if _, exists := received.Arguments[timeoutArgument]; exists {
		t.Fatalf("arguments = %#v, want timeout removed", received.Arguments)
	}

	if _, exists := call.Arguments[timeoutArgument]; !exists {
		t.Fatalf("original arguments = %#v, want unchanged", call.Arguments)
	}
}

func TestTimeoutArgumentBypassesUnderlyingSchema(t *testing.T) {
	t.Parallel()

	type input struct {
		Value string `json:"value"`
	}

	underlying := adktool.MustNew(
		"test",
		"Test tool",
		func(_ *kit.RunContext, value input) (string, error) {
			return value.Value, nil
		},
	)

	wrapped, err := Timeout(Config{
		Timeout:    time.Second,
		MaxTimeout: time.Minute,
	})(agent.BuildContext{}, underlying)
	if err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}

	output, err := wrapped.Execute(
		kit.NewRunContext(context.Background()),
		kit.NewToolCall("call", "test", map[string]any{
			"value":         "done",
			timeoutArgument: float64(2),
		}),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := output.Content[0].Text.Text, "done"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTimeoutReturnsToolTimeout(t *testing.T) {
	t.Parallel()

	wrapped, err := Timeout(Config{
		Timeout:    20 * time.Millisecond,
		MaxTimeout: time.Second,
	})(agent.BuildContext{}, blockingTool())
	if err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}

	_, err = wrapped.Execute(
		kit.NewRunContext(context.Background()),
		kit.NewToolCall("call", "test", nil),
	)
	if err == nil || !strings.Contains(err.Error(), "tool execution timed out after 20ms") {
		t.Fatalf("Execute() error = %v, want tool timeout", err)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, must not expose context deadline", err)
	}
}

func TestTimeoutPropagatesParentCancellation(t *testing.T) {
	t.Parallel()

	wrapped, err := Timeout(Config{
		Timeout:    time.Minute,
		MaxTimeout: time.Minute,
	})(agent.BuildContext{}, blockingTool())
	if err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = wrapped.Execute(
		kit.NewRunContext(ctx),
		kit.NewToolCall("call", "test", nil),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
}

func TestTimeoutRejectsInvalidOverride(t *testing.T) {
	t.Parallel()

	tests := []any{float64(0), 1.5, "5", float64(61)}
	for _, value := range tests {
		wrapped, err := Timeout(Config{
			Timeout:    time.Second,
			MaxTimeout: time.Minute,
		})(agent.BuildContext{}, testTool{
			definition: kit.ToolDefinition{Name: "test"},
			execute: func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error) {
				t.Fatal("underlying tool was called")

				return kit.ToolOutput{}, nil
			},
		})
		if err != nil {
			t.Fatalf("Timeout() error = %v", err)
		}

		_, err = wrapped.Execute(
			kit.NewRunContext(context.Background()),
			kit.NewToolCall("call", "test", map[string]any{timeoutArgument: value}),
		)
		if err == nil {
			t.Fatalf("Execute(%#v) error = nil", value)
		}
	}
}

func timeoutProperty(t *testing.T, definition kit.ToolDefinition) map[string]any {
	t.Helper()

	properties, ok := definition.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v, want map", definition.Schema["properties"])
	}

	property, ok := properties[timeoutArgument].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want map", timeoutArgument, properties[timeoutArgument])
	}

	return property
}

func blockingTool() kit.Tool {
	return testTool{
		definition: kit.ToolDefinition{Name: "test"},
		execute: func(run *kit.RunContext, _ kit.ToolCall) (kit.ToolOutput, error) {
			<-run.Done()

			return kit.ToolOutput{}, run.Err()
		},
	}
}
