package tools

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestWrapRejectsExistingArgument(t *testing.T) {
	t.Parallel()

	_, err := wrap(
		testTool{definition: kit.ToolDefinition{
			Name: "test",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"wrapper_argument": map[string]any{"type": "string"},
				},
			},
		}},
		[]argument{{name: "wrapper_argument", schema: map[string]any{"type": "string"}}},
		func(
			*kit.RunContext,
			kit.ToolCall,
			nextTool,
		) (kit.ToolOutput, error) {
			return kit.ToolOutput{}, nil
		},
	)
	if err == nil {
		t.Fatal("wrap() error = nil, want argument conflict")
	}
}

func TestWrapInvokesHandler(t *testing.T) {
	t.Parallel()

	called := false

	wrapped, err := wrap(
		testTool{
			definition: kit.ToolDefinition{Name: "test"},
			execute: func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error) {
				return kit.NewToolOutput(kit.NewTextContent("done")), nil
			},
		},
		nil,
		func(run *kit.RunContext, call kit.ToolCall, next nextTool) (kit.ToolOutput, error) {
			called = true

			return next(run, call)
		},
	)
	if err != nil {
		t.Fatalf("wrap() error = %v", err)
	}

	if _, err := wrapped.Execute(
		kit.NewRunContext(context.Background()),
		kit.NewToolCall("call", "test", nil),
	); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !called {
		t.Fatal("handler was not called")
	}
}
