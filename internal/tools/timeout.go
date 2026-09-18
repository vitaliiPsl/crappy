package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
)

const timeoutArgument = "timeout_seconds"

func Timeout(config Config) agent.ToolMiddleware {
	return func(_ agent.BuildContext, tool kit.Tool) (kit.Tool, error) {
		arguments := []argument{{
			name:   timeoutArgument,
			schema: timeoutSchema(config),
		}}

		return wrap(tool, arguments, func(
			run *kit.RunContext,
			call kit.ToolCall,
			next nextTool,
		) (kit.ToolOutput, error) {
			return executeWithTimeout(run, call, next, config)
		})
	}
}

func executeWithTimeout(
	run *kit.RunContext,
	call kit.ToolCall,
	next nextTool,
	config Config,
) (kit.ToolOutput, error) {
	requested, err := parseTimeout(call.Arguments)
	if err != nil {
		return kit.ToolOutput{}, err
	}

	timeout, err := config.resolveTimeout(requested)
	if err != nil {
		return kit.ToolOutput{}, err
	}

	ctx, cancel := context.WithTimeout(run.Context, timeout)
	defer cancel()

	child := *run
	child.Context = ctx

	output, err := next(&child, call)

	if parentErr := run.Err(); parentErr != nil {
		return kit.ToolOutput{}, parentErr
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return kit.ToolOutput{}, fmt.Errorf("tool execution timed out after %s", timeout)
	}

	return output, err
}

func (c Config) resolveTimeout(requested *int64) (time.Duration, error) {
	if requested == nil {
		return c.Timeout, nil
	}

	if *requested <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", timeoutArgument)
	}

	maxSeconds := int64(c.MaxTimeout / time.Second)
	if *requested > maxSeconds {
		return 0, fmt.Errorf(
			"%s exceeds the maximum of %d seconds",
			timeoutArgument,
			maxSeconds,
		)
	}

	return time.Duration(*requested) * time.Second, nil
}

func parseTimeout(arguments map[string]any) (*int64, error) {
	value, exists := arguments[timeoutArgument]
	if !exists {
		return nil, nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", timeoutArgument, err)
	}

	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return nil, fmt.Errorf("%s must be a positive integer", timeoutArgument)
	}

	return &seconds, nil
}

func timeoutSchema(config Config) map[string]any {
	return map[string]any{
		"type":        "integer",
		"minimum":     int64(1),
		"maximum":     int64(config.MaxTimeout / time.Second),
		"description": fmt.Sprintf("Execution timeout in seconds (default %d)", int64(config.Timeout/time.Second)),
	}
}
