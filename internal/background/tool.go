package background

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

const timeoutArgument = "background_timeout_seconds"

func NewTool(manager *Manager, sessionID string, underlying kit.Tool) (kit.Tool, error) {
	maxSeconds := int64(manager.MaxTimeout() / time.Second)

	return tool.Wrap(underlying, []tool.Argument{{
		Name: timeoutArgument,
		Schema: map[string]any{
			"type":        "integer",
			"minimum":     1,
			"maximum":     maxSeconds,
			"description": "Run in the background with this independent timeout in seconds; omit to run in the foreground",
		},
	}}, func(run *kit.RunContext, call kit.ToolCall, next tool.Next) (kit.ToolOutput, error) {
		value, background := call.Arguments[timeoutArgument]
		if !background {
			return next(run, call)
		}

		timeout, err := parseTimeout(value, maxSeconds)
		if err != nil {
			return kit.ToolOutput{}, err
		}

		record, err := manager.Start(run.Context, sessionID, call.Name, timeout, func(ctx context.Context) (kit.ToolOutput, error) {
			return next(kit.NewRunContext(ctx), call)
		})
		if err != nil {
			return kit.ToolOutput{}, err
		}

		return kit.NewStructuredToolOutput(record), nil
	})
}

func parseTimeout(value any, maxSeconds int64) (time.Duration, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", timeoutArgument, err)
	}

	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil || seconds <= 0 || seconds > maxSeconds {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", timeoutArgument, maxSeconds)
	}

	return time.Duration(seconds) * time.Second, nil
}
