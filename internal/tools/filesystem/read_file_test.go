package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileTextRange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, path, []byte("first\n\nthird\nfourth\n"))

	output, err := readFile(context.Background(), path, 2, 3)
	if err != nil {
		t.Fatalf("readFile() error = %v", err)
	}

	if got, want := output, "\nthird"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestReadFileRejectsBinary(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.bin")
	writeTestFile(t, path, []byte{0xff, 0xfe, 0xfd})

	if _, err := readFile(context.Background(), path, 1, 0); err == nil {
		t.Fatal("readFile() error = nil, want text validation error")
	}
}

func TestReadFileTruncatesText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.txt")
	writeTestFile(t, path, []byte(strings.Repeat("0123456789\n", 10_000)))

	output, err := readFile(context.Background(), path, 1, 0)
	if err != nil {
		t.Fatalf("readFile() error = %v", err)
	}

	if len(output) > maxTextReadBytes {
		t.Fatalf("len(output) = %d, want at most %d", len(output), maxTextReadBytes)
	}

	if !strings.Contains(output, "[output truncated; continue reading from line ") {
		t.Fatalf("output does not contain truncation marker: %q", output[len(output)-100:])
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
