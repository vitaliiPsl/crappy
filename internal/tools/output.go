package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/harness/agent"
)

const (
	outputHeadPercent  = 60
	outputMarkerFormat = "[tool output exceeded the size limit and was truncated; %d lines (%d bytes) omitted]\n" +
		"[full output: %s; use read_file with start/end to inspect]"
)

// OutputCap stores oversized text and returns a bounded preview of each text content or error.
func OutputCap(config Config, putBlob func(context.Context, string, []byte) (string, error)) agent.ToolMiddleware {
	return func(build agent.BuildContext, underlying kit.Tool) (kit.Tool, error) {
		capper := outputCap{
			limit:     config.MaxOutputBytes,
			putBlob:   putBlob,
			sessionID: build.Request.SessionID,
		}

		return tool.Wrap(underlying, nil, func(run *kit.RunContext, call kit.ToolCall, next tool.Next) (kit.ToolOutput, error) {
			output, toolErr := next(run, call)
			if run.Err() != nil {
				return output, toolErr
			}

			capped, err := capper.capOutput(run.Context, output)
			if err != nil {
				return kit.ToolOutput{}, &outputError{
					message: fmt.Sprintf("cap tool output: %v", err),
					cause:   errors.Join(err, toolErr),
				}
			}

			return capped, capper.capError(run.Context, toolErr)
		})
	}
}

type outputCap struct {
	limit     int
	putBlob   func(context.Context, string, []byte) (string, error)
	sessionID string
}

func (c outputCap) capOutput(ctx context.Context, output kit.ToolOutput) (kit.ToolOutput, error) {
	contents := output.Content
	changed := false

	for index, content := range contents {
		if content.Type != kit.ContentTypeText || content.Text == nil || len(content.Text.Text) <= c.limit {
			continue
		}

		capped, err := c.cap(ctx, content.Text.Text)
		if err != nil {
			return kit.ToolOutput{}, err
		}

		if !changed {
			contents = append([]kit.Content(nil), contents...)
			changed = true
		}

		contents[index] = kit.NewTextContent(capped)
	}

	output.Content = contents

	return output, nil
}

func (c outputCap) capError(ctx context.Context, toolErr error) error {
	if toolErr == nil || len(toolErr.Error()) <= c.limit {
		return toolErr
	}

	capped, err := c.cap(ctx, toolErr.Error())
	if err != nil {
		return &outputError{
			message: fmt.Sprintf("cap tool error: %v", err),
			cause:   errors.Join(toolErr, err),
		}
	}

	return &outputError{message: capped, cause: toolErr}
}

func (c outputCap) cap(ctx context.Context, value string) (string, error) {
	path, err := c.putBlob(ctx, c.sessionID, []byte(value))
	if err != nil {
		return "", fmt.Errorf("store full output: %w", err)
	}

	return cappedPreview(value, path, c.limit)
}

type outputError struct {
	message string
	cause   error
}

func (e *outputError) Error() string {
	return e.message
}

func (e *outputError) Unwrap() error {
	return e.cause
}

func cappedPreview(value, path string, limit int) (string, error) {
	value = strings.ToValidUTF8(value, string(utf8.RuneError))
	marker := omissionMarker(value, path)

	available := limit - len(marker) - 2
	if available <= 0 {
		return "", errors.New("output limit is too small to include the full output path")
	}

	headLimit := available * outputHeadPercent / 100
	tailLimit := available - headLimit
	headEnd := utf8PrefixBoundary(value, headLimit)
	tailStart := utf8SuffixBoundary(value, tailLimit)
	marker = omissionMarker(value[headEnd:tailStart], path)

	return value[:headEnd] + "\n" + marker + "\n" + value[tailStart:], nil
}

func utf8PrefixBoundary(value string, limit int) int {
	boundary := min(limit, len(value))
	for boundary > 0 && boundary < len(value) && !utf8.RuneStart(value[boundary]) {
		boundary--
	}

	return boundary
}

func utf8SuffixBoundary(value string, limit int) int {
	boundary := max(len(value)-limit, 0)
	for boundary < len(value) && !utf8.RuneStart(value[boundary]) {
		boundary++
	}

	return boundary
}

func omissionMarker(value, path string) string {
	return fmt.Sprintf(outputMarkerFormat, lineCount(value), len(value), path)
}

func lineCount(value string) int {
	if value == "" {
		return 0
	}

	lines := strings.Count(value, "\n")
	if !strings.HasSuffix(value, "\n") {
		lines++
	}

	return lines
}
