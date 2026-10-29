package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	maxInstructionFileBytes  = 64 * 1024
	maxInstructionTotalBytes = 256 * 1024

	instructionFilesHeader = `# Session Instructions

The files below provide local guidance for this session. Each applies to its directory and descendants; deeper instructions take precedence over ancestor instructions. Explicit user instructions take precedence over this guidance.

Before working in a subdirectory, check for additional AGENTS.md or CLAUDE.md files.
`
)

var instructionFileNames = [...]string{"AGENTS.md", "CLAUDE.md"}

type instructionFile struct {
	path    string
	content string
}

func instructionFiles(workdir string) string {
	files := loadInstructionFiles(workdir)
	if len(files) == 0 {
		return ""
	}

	var output strings.Builder
	output.WriteString(instructionFilesHeader)

	for _, file := range files {
		fmt.Fprintf(&output, "\n\n## %s\n\n%s", file.path, file.content)
	}

	return output.String()
}

func loadInstructionFiles(workdir string) []instructionFile {
	var (
		files []instructionFile
		total int
	)

	for _, directory := range ancestorDirectories(workdir) {
		for _, name := range instructionFileNames {
			path := filepath.Join(directory, name)

			info, err := os.Stat(path)
			if err != nil || info.IsDir() || info.Size() > maxInstructionFileBytes {
				continue
			}

			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			content := strings.TrimSpace(string(data))
			if content == "" || total+len(content) > maxInstructionTotalBytes {
				continue
			}

			files = append(files, instructionFile{path: path, content: content})
			total += len(content)
		}
	}

	return files
}

func ancestorDirectories(workdir string) []string {
	var directories []string

	for directory := filepath.Clean(workdir); ; directory = filepath.Dir(directory) {
		directories = append(directories, directory)

		if parent := filepath.Dir(directory); parent == directory {
			break
		}
	}

	slices.Reverse(directories)

	return directories
}
