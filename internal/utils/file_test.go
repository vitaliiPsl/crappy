package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedFileDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "file.yaml")
	if err := SeedFile(path, []byte("first\n")); err != nil {
		t.Fatalf("SeedFile() error = %v", err)
	}

	if err := SeedFile(path, []byte("second\n")); err != nil {
		t.Fatalf("SeedFile() second error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got, want := string(data), "first\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode = %o, want %o", got, want)
	}
}
