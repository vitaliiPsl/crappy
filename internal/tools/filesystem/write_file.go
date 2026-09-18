package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"

	"github.com/vitaliipsl/crappy/internal/utils"
)

const (
	writeFileName        = "write_file"
	writeFileDescription = "Create a file or replace its complete contents. Use edit_file for targeted changes to an existing file. Relative paths are resolved from the working directory."

	maxWriteFileBytes = 1024 * 1024
)

type writeFileInput struct {
	Path    string `json:"path" jsonschema:"File path to write, absolute or relative to the working directory"`
	Content string `json:"content" jsonschema:"Complete file contents"`
}

func NewWriteFile() kit.Tool {
	return tool.MustNew(
		writeFileName,
		writeFileDescription,
		func(run *kit.RunContext, input writeFileInput) (string, error) {
			path, err := utils.ResolvePath(input.Path)
			if err != nil {
				return "", err
			}

			return writeFile(run.Context, path, input.Content)
		},
	)
}

func writeFile(ctx context.Context, path, content string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if len(content) > maxWriteFileBytes {
		return "", fmt.Errorf("content exceeds the write limit of %d bytes", maxWriteFileBytes)
	}

	if !utf8.ValidString(content) {
		return "", errors.New("content is not valid UTF-8 text")
	}

	_, statErr := os.Stat(path)

	existed := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat file: %w", statErr)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if existed {
		return fmt.Sprintf("Overwrote %s", path), nil
	}

	return fmt.Sprintf("Created %s", path), nil
}
