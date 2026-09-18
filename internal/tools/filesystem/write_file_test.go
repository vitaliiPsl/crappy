package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileCreatesAndOverwrites(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "file.txt")

	result, err := writeFile(context.Background(), path, "first")
	if err != nil {
		t.Fatalf("writeFile() error = %v", err)
	}

	if !strings.HasPrefix(result, "Created ") {
		t.Fatalf("writeFile() = %q, want created result", result)
	}

	result, err = writeFile(context.Background(), path, "second")
	if err != nil {
		t.Fatalf("writeFile() overwrite error = %v", err)
	}

	if !strings.HasPrefix(result, "Overwrote ") {
		t.Fatalf("writeFile() = %q, want overwritten result", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if got, want := string(data), "second"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestWriteFileRejectsOversizedContent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")

	_, err := writeFile(
		context.Background(),
		path,
		strings.Repeat("x", maxWriteFileBytes+1),
	)
	if err == nil {
		t.Fatal("writeFile() error = nil, want size error")
	}
}

func TestWriteFileRejectsInvalidText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	if _, err := writeFile(context.Background(), path, string([]byte{0xff})); err == nil {
		t.Fatal("writeFile() error = nil, want text validation error")
	}
}
