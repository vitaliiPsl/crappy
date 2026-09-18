//go:build unix

package bash

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecuteTerminatesBackgroundProcessesAfterExit(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")

	started := time.Now()

	output, err := execute(context.Background(), "sleep 40 & echo $!")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if elapsed := time.Since(started); elapsed > waitDelay+5*time.Second {
		t.Fatalf("execute() took %v, want it bounded by WaitDelay", elapsed)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(output, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("parse background PID from %q: %v", output, err)
	}

	if !processGone(pid) {
		t.Fatalf("background process %d survived execute()", pid)
	}
}

func TestExecuteTerminatesBackgroundProcessesOnCancel(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")

	pidFile := filepath.Join(t.TempDir(), "pid")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := execute(ctx, "sleep 40 & echo $! > "+pidFile+"; sleep 30"); err == nil {
		t.Fatal("execute() error = nil, want cancellation error")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read PID file: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse background PID from %q: %v", data, err)
	}

	if !processGone(pid) {
		t.Fatalf("background process %d survived cancellation", pid)
	}
}

func processGone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}

		time.Sleep(10 * time.Millisecond)
	}

	return false
}
