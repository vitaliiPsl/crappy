package background

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/tools"
)

func TestToolBackgroundTimeoutExceedsForegroundMiddlewareLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	manager := newTestManager(t)
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	underlying := tool.MustNew("work", "Work", func(run *kit.RunContext, _ struct{}) (string, error) {
		started <- run.Context

		select {
		case <-release:
			return "finished", nil
		case <-run.Done():
			return "", run.Err()
		}
	})

	wrapped, err := NewTool(manager, "session", underlying)
	if err != nil {
		t.Fatal(err)
	}

	wrapped, err = tools.Timeout(tools.Config{Timeout: 100 * time.Millisecond, MaxTimeout: 200 * time.Millisecond})(agent.BuildContext{}, wrapped)
	if err != nil {
		t.Fatal(err)
	}

	caller, disconnect := context.WithCancel(ctx)

	output, err := wrapped.Execute(kit.NewRunContext(caller), kit.NewToolCall("call", "work", map[string]any{timeoutArgument: 60}))
	if err != nil {
		t.Fatal(err)
	}

	disconnect()

	var running context.Context
	select {
	case running = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	deadline, ok := running.Deadline()
	if running.Err() != nil || !ok || time.Until(deadline) < 30*time.Second {
		t.Fatalf("background inherited the foreground timeout: deadline=%s, error=%v", deadline, running.Err())
	}

	data, err := json.Marshal(output.Structured)
	if err != nil {
		t.Fatal(err)
	}

	var admitted Record
	if err := json.Unmarshal(data, &admitted); err != nil {
		t.Fatal(err)
	}

	close(release)

	if finished := receiveCompletion(ctx, t, manager); finished.ID != admitted.ID || finished.State != Completed {
		t.Fatalf("job did not complete after launcher cancellation: %+v", finished)
	}
}

func TestToolForegroundAndInvalidBackgroundTimeouts(t *testing.T) {
	t.Parallel()

	manager := NewManager(90 * time.Second)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	underlying := tool.MustNew("work", "Work", func(_ *kit.RunContext, input struct {
		Value string `json:"value"`
	}) (string, error) {
		return input.Value, nil
	})

	wrapped, err := NewTool(manager, "session", underlying)
	if err != nil {
		t.Fatal(err)
	}

	properties := wrapped.Definition().Schema["properties"].(map[string]any)

	background := properties[timeoutArgument].(map[string]any)
	if maximum := background["maximum"]; maximum != int64(90) {
		t.Fatalf("tool schema maximum = %v, want 90", maximum)
	}

	output, err := wrapped.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", "work", map[string]any{"value": "foreground"}))
	if err != nil || kit.ContentsText(output.Content) != "foreground" {
		t.Fatalf("foreground = %+v, %v", output, err)
	}

	for _, value := range []any{nil, 0, -1, 1.5, true, "60", int64(manager.MaxTimeout()/time.Second) + 1, int64(1 << 62)} {
		_, err := wrapped.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", "work", map[string]any{
			"value": "invalid", timeoutArgument: value,
		}))
		if err == nil {
			t.Fatalf("accepted background timeout %v", value)
		}
	}

	if records, err := manager.List(t.Context(), "session"); err != nil || len(records) != 0 {
		t.Fatalf("foreground or rejected input created jobs: %+v, %v", records, err)
	}
}
