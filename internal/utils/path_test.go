package utils

import (
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)

	resolved, err := ResolvePath(filepath.Join("nested", "file.txt"))
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}

	want := filepath.Join(workingDirectory, "nested", "file.txt")
	if resolved != want {
		t.Fatalf("ResolvePath() = %q, want %q", resolved, want)
	}
}

func TestResolvePathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := ResolvePath("~/file.txt")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}

	want := filepath.Join(home, "file.txt")
	if resolved != want {
		t.Fatalf("ResolvePath() = %q, want %q", resolved, want)
	}
}

func TestResolvePathFromFile(t *testing.T) {
	directory := t.TempDir()

	resolved, err := ResolvePathFromFile(
		filepath.Join(directory, "settings.yaml"),
		filepath.Join("config", "config.yaml"),
	)
	if err != nil {
		t.Fatalf("ResolvePathFromFile() error = %v", err)
	}

	want := filepath.Join(directory, "config", "config.yaml")
	if resolved != want {
		t.Fatalf("ResolvePathFromFile() = %q, want %q", resolved, want)
	}
}
