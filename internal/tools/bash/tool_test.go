package bash

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	output, err := execute(context.Background(), t.TempDir(), "printf hello")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "hello"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}

func TestExecuteUsesWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()

	output, err := execute(context.Background(), workingDirectory, "pwd -P")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	want, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		t.Fatal(err)
	}

	if output != want {
		t.Fatalf("execute() = %q, want %q", output, want)
	}
}

func TestExecuteDirectoryChangesStayInSubprocess(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()

	nested := filepath.Join(workdir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	parent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	output, err := execute(context.Background(), workdir, "cd nested && pwd -P")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}

	if output != want {
		t.Fatalf("shell workdir = %q, want %q", output, want)
	}

	output, err = execute(context.Background(), workdir, "pwd -P")
	if err != nil {
		t.Fatal(err)
	}

	want, err = filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}

	if output != want {
		t.Fatalf("next shell workdir = %q, want %q", output, want)
	}

	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if current != parent {
		t.Fatalf("assistant workdir changed: %q, want %q", current, parent)
	}
}

func TestExecuteIncludesOutputOnFailure(t *testing.T) {
	_, err := execute(context.Background(), t.TempDir(), "echo oops >&2; exit 7")
	if err == nil {
		t.Fatal("execute() error = nil")
	}

	if !strings.Contains(err.Error(), "exit status 7") || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("execute() error = %q", err)
	}
}

func TestExecuteCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := execute(ctx, t.TempDir(), "sleep 10"); err == nil {
		t.Fatal("execute() error = nil")
	}
}

func TestExecuteUsesConfiguredShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")

	output, err := execute(context.Background(), t.TempDir(), "printf configured")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "configured"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}

func TestExecuteDefaultsToSh(t *testing.T) {
	t.Setenv("SHELL", "")

	output, err := execute(context.Background(), t.TempDir(), "printf fallback")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "fallback"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}
