package filesystem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/internal/utils"
)

const (
	readFileName        = "read_file"
	readFileDescription = "Read a UTF-8 text file by one-based line range. Relative paths are resolved from the working directory."

	maxTextReadBytes        = 64 * 1024
	maxReadLineBytes        = 1024 * 1024
	truncationMarkerReserve = 128
)

type readFileInput struct {
	Path  string `json:"path" jsonschema:"Text file path, absolute or relative to the working directory"`
	Start *int   `json:"start,omitempty" jsonschema:"First line to return, one-based and inclusive; defaults to 1"`
	End   *int   `json:"end,omitempty" jsonschema:"Last line to return, one-based and inclusive; defaults to the end of the file"`
}

func NewReadFile() kit.Tool {
	return tool.MustNew(
		readFileName,
		readFileDescription,
		func(run *kit.RunContext, input readFileInput) (string, error) {
			path, err := utils.ResolvePath(input.Path)
			if err != nil {
				return "", err
			}

			start, end, err := lineRange(input.Start, input.End)
			if err != nil {
				return "", err
			}

			return readFile(run.Context, path, start, end)
		},
	)
}

func readFile(ctx context.Context, path string, start, end int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}

	defer func() { _ = file.Close() }()

	return readText(ctx, file, start, end)
}

func lineRange(startValue, endValue *int) (int, int, error) {
	start := 1
	if startValue != nil {
		start = *startValue
	}

	if start <= 0 {
		return 0, 0, errors.New("start must be greater than zero")
	}

	end := 0
	if endValue != nil {
		end = *endValue
		if end < start {
			return 0, 0, errors.New("end must be greater than or equal to start")
		}
	}

	return start, end, nil
}

func readText(ctx context.Context, file *os.File, start, end int) (string, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, maxTextReadBytes), maxReadLineBytes)

	var output strings.Builder

	line, selected := 0, 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		line++

		value := scanner.Text()
		if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
			return "", errors.New("file is not valid UTF-8 text")
		}

		if line < start {
			continue
		}

		if end > 0 && line > end {
			break
		}

		additional := len(value)
		if selected > 0 {
			additional++
		}

		if output.Len()+additional > maxTextReadBytes-truncationMarkerReserve {
			if selected == 0 {
				return "", fmt.Errorf("line %d exceeds the text read limit of %d bytes", line, maxTextReadBytes)
			}

			return fmt.Sprintf(
				"%s\n[output truncated; continue reading from line %d]",
				output.String(),
				line,
			), nil
		}

		if selected > 0 {
			output.WriteByte('\n')
		}

		output.WriteString(value)

		selected++
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if line == 0 && start == 1 {
		return "", nil
	}

	if line < start {
		return "", fmt.Errorf("start line %d is beyond file length", start)
	}

	return output.String(), nil
}
