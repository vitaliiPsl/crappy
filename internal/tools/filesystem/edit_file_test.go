package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileReplacesUniqueText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, path, []byte("before old after"))

	result, err := editFile(context.Background(), path, "old", "new", false)
	if err != nil {
		t.Fatalf("editFile() error = %v", err)
	}

	if !strings.Contains(result, "1 replacement(s)") {
		t.Fatalf("editFile() = %q, want replacement count", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if got, want := string(data), "before new after"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileRequiresUniqueText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, path, []byte("old and old"))

	if _, err := editFile(context.Background(), path, "old", "new", false); err == nil {
		t.Fatal("editFile() error = nil, want multiple match error")
	}

	if _, err := editFile(context.Background(), path, "old", "new", true); err != nil {
		t.Fatalf("editFile() replace all error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if got, want := string(data), "new and new"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFileRejectsBinaryFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.bin")
	writeTestFile(t, path, []byte{0xff, 0xfe, 0xfd})

	if _, err := editFile(context.Background(), path, "old", "new", false); err == nil {
		t.Fatal("editFile() error = nil, want binary file error")
	}
}
