package bash

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	output, err := execute(context.Background(), "printf hello")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "hello"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}

func TestExecuteUsesWorkingDirectory(t *testing.T) {
	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)

	output, err := execute(context.Background(), "pwd")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, filepath.Clean(workingDirectory); got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}

func TestExecuteIncludesOutputOnFailure(t *testing.T) {
	_, err := execute(context.Background(), "echo oops >&2; exit 7")
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

	if _, err := execute(ctx, "sleep 10"); err == nil {
		t.Fatal("execute() error = nil")
	}
}

func TestExecuteUsesConfiguredShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")

	output, err := execute(context.Background(), "printf configured")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "configured"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}

func TestExecuteDefaultsToSh(t *testing.T) {
	t.Setenv("SHELL", "")

	output, err := execute(context.Background(), "printf fallback")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if got, want := output, "fallback"; got != want {
		t.Fatalf("execute() = %q, want %q", got, want)
	}
}
