package filesystem

import (
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
	editFileName        = "edit_file"
	editFileDescription = "Edit an existing text file by replacing exact text. The old text must occur exactly once unless replace_all is true. Relative paths are resolved from the working directory."

	maxEditFileBytes = 1024 * 1024
)

type editFileInput struct {
	Path       string `json:"path" jsonschema:"File path to edit, absolute or relative to the working directory"`
	OldString  string `json:"old_string" jsonschema:"Exact text to replace"`
	NewString  string `json:"new_string" jsonschema:"Replacement text; may be empty to delete the old text"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"Replace every occurrence instead of requiring exactly one"`
}

func NewEditFile() kit.Tool {
	return tool.MustNew(
		editFileName,
		editFileDescription,
		func(run *kit.RunContext, input editFileInput) (string, error) {
			path, err := utils.ResolvePath(input.Path)
			if err != nil {
				return "", err
			}

			return editFile(
				run.Context,
				path,
				input.OldString,
				input.NewString,
				input.ReplaceAll,
			)
		},
	)
}

func editFile(
	ctx context.Context,
	path, oldString, newString string,
	replaceAll bool,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if oldString == "" {
		return "", errors.New("old_string must not be empty")
	}

	if oldString == newString {
		return "", errors.New("old_string and new_string must differ")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	if info.Size() > maxEditFileBytes {
		return "", fmt.Errorf("file exceeds the edit limit of %d bytes", maxEditFileBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if len(data) > maxEditFileBytes {
		return "", fmt.Errorf("file exceeds the edit limit of %d bytes", maxEditFileBytes)
	}

	if !utf8.Valid(data) {
		return "", errors.New("file is not valid UTF-8 text")
	}

	content := string(data)

	matches := strings.Count(content, oldString)
	if matches == 0 {
		return "", errors.New("old_string not found")
	}

	if !replaceAll && matches > 1 {
		return "", fmt.Errorf("old_string matches %d locations; provide a unique match or set replace_all", matches)
	}

	replacements := 1
	if replaceAll {
		replacements = matches
	}

	updated := strings.Replace(content, oldString, newString, replacements)
	if len(updated) > maxEditFileBytes {
		return "", fmt.Errorf("edited file exceeds the edit limit of %d bytes", maxEditFileBytes)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("Edited %s (%d replacement(s))", path, replacements), nil
}
