package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestToolConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
	}{
		{name: "read", tool: NewReadFile(t.TempDir()).Definition().Name},
		{name: "write", tool: NewWriteFile(t.TempDir()).Definition().Name},
		{name: "edit", tool: NewEditFile(t.TempDir()).Definition().Name},
	}

	want := []string{readFileName, writeFileName, editFileName}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.tool != want[index] {
				t.Fatalf("tool name = %q, want %q", test.tool, want[index])
			}
		})
	}
}

func TestNewProvidesFilesystemTools(t *testing.T) {
	t.Parallel()

	tools := New(t.TempDir())

	want := []string{readFileName, writeFileName, editFileName}
	if len(tools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(want))
	}

	for index, tool := range tools {
		if got := tool.Definition().Name; got != want[index] {
			t.Fatalf("tool %d name = %q, want %q", index, got, want[index])
		}
	}
}

func TestToolsUseWorkingDirectory(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	available := New(workdir)

	tools := make(map[string]kit.Tool, len(available))
	for _, tool := range available {
		tools[tool.Definition().Name] = tool
	}

	for _, path := range []string{"relative.txt", filepath.Join(t.TempDir(), "absolute.txt")} {
		t.Run(path, func(t *testing.T) {
			for _, call := range []kit.ToolCall{
				kit.NewToolCall("write", writeFileName, map[string]any{"path": path, "content": "before"}),
				kit.NewToolCall("read", readFileName, map[string]any{"path": path}),
				kit.NewToolCall("edit", editFileName, map[string]any{"path": path, "old_string": "before", "new_string": "after"}),
			} {
				output, err := tools[call.Name].Execute(kit.NewRunContext(context.Background()), call)
				if err != nil {
					t.Fatalf("%s: %v", call.Name, err)
				}

				if call.Name == readFileName && output.Content[0].Text.Text != "before" {
					t.Fatalf("read output = %+v", output)
				}
			}

			if !filepath.IsAbs(path) {
				path = filepath.Join(workdir, path)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if string(data) != "after" {
				t.Fatalf("file content = %q, want after", data)
			}
		})
	}
}
