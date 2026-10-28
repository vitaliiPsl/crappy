package utils

import (
	"os"
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

func TestResolvePathFromDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "file.txt")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "relative", path: "file.txt", want: filepath.Join(directory, "file.txt")},
		{name: "nested", path: filepath.Join("nested", "file.txt"), want: filepath.Join(directory, "nested", "file.txt")},
		{name: "parent", path: filepath.Join("..", "file.txt"), want: filepath.Join(filepath.Dir(directory), "file.txt")},
		{name: "absolute", path: absolute, want: absolute},
		{name: "home", path: "~/file.txt", want: filepath.Join(home, "file.txt")},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolvePathFromDirectory(directory, test.path)
			if err != nil {
				t.Fatal(err)
			}

			if got != test.want {
				t.Fatalf("resolved path = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := ResolvePathFromDirectory(directory, ""); err == nil {
		t.Fatal("empty path was accepted")
	}
}
