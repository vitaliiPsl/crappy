package filesystem

import "testing"

func TestToolConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
	}{
		{name: "read", tool: NewReadFile().Definition().Name},
		{name: "write", tool: NewWriteFile().Definition().Name},
		{name: "edit", tool: NewEditFile().Definition().Name},
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

	tools := New()

	want := []string{readFileName, writeFileName, editFileName}
	if len(tools) != len(want) {
		t.Fatalf("len(New()) = %d, want %d", len(tools), len(want))
	}

	for index, tool := range tools {
		if got := tool.Definition().Name; got != want[index] {
			t.Fatalf("tool %d name = %q, want %q", index, got, want[index])
		}
	}
}
