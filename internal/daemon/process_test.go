package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/api"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestManagerProcess(t *testing.T) {
	role := os.Getenv("CRAPPY_TEST_PROCESS")
	if role == "" {
		return
	}

	root := os.Getenv("CRAPPY_TEST_ROOT")
	if role == "client" {
		client := api.NewClient(filepath.Join(root, "runtime", "socket"), root)
		defer client.Close()

		id, err := client.Execute(t.Context(), os.Getenv("CRAPPY_TEST_SESSION"), kit.NewTextContent("wait"))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(root, "accepted"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}

		return
	}

	fixture := newServerFixture(t, root, execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		var input kit.Message
		if err := json.Unmarshal(params.Input, &input); err != nil {
			return err
		}

		if kit.ContentsText(input.Content) == "wait" {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()

			for {
				if _, err := os.Stat(filepath.Join(root, "release")); err == nil {
					break
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
		}

		return commitResult(ctx, params, "done")
	}))
	<-fixture.done
}

func TestManagerDetachedExecutionAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess acceptance test")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()

	root := newSocketRoot(t)
	environment := append(os.Environ(), "CRAPPY_TEST_PROCESS=server", "CRAPPY_TEST_ROOT="+root)
	manager := NewManager(filepath.Join(root, "runtime"), []string{os.Args[0], "-test.run=^TestManagerProcess$"}, environment)
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := manager.Stop(stopContext); err != nil {
			t.Error(err)
		}
	})

	errors := make(chan error, 3)

	var starts sync.WaitGroup
	for range 3 {
		starts.Go(func() { errors <- manager.Ensure(ctx) })
	}

	starts.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			log, _ := os.ReadFile(manager.LogPath())
			t.Fatalf("start: %v\n%s", err, log)
		}
	}

	health, err := manager.Status(ctx)
	if err != nil || health.PID == 0 || health.PID == os.Getpid() {
		t.Fatalf("detached health=%+v, %v", health, err)
	}

	client := api.NewClient(manager.SocketPath(), root)
	defer client.Close()

	session, err := client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManagerProcess$")

	child.Env = append(os.Environ(), "CRAPPY_TEST_PROCESS=client", "CRAPPY_TEST_ROOT="+root, "CRAPPY_TEST_SESSION="+session.ID)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("client: %v\n%s", err, output)
	}

	accepted, err := os.ReadFile(filepath.Join(root, "accepted"))
	if err != nil {
		t.Fatal(err)
	}

	id := string(accepted)
	if status, err := client.GetExecutionStatus(ctx, session.ID, id); err != nil || status != execution.Running {
		t.Fatalf("after client process exit=%s, %v", status, err)
	}

	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("detached result=%s, %v", status, err)
	}

	if err := os.Remove(filepath.Join(root, "release")); err != nil {
		t.Fatal(err)
	}

	interrupted, err := client.Execute(ctx, session.ID, kit.NewTextContent("wait"))
	if err != nil {
		t.Fatal(err)
	}

	queued, err := client.Execute(ctx, session.ID, kit.NewTextContent("quick"))
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if status, err := manager.Status(ctx); err != nil || status.PID != 0 {
		t.Fatalf("stopped=%+v, %v", status, err)
	}

	client.Close()

	if err := manager.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	client = api.NewClient(manager.SocketPath(), root)
	defer client.Close()

	for id, want := range map[string]execution.Status{interrupted: execution.Interrupted, queued: execution.Queued} {
		if status, err := client.GetExecutionStatus(ctx, session.ID, id); err != nil || status != want {
			t.Fatalf("restored %s=%s, %v; want %s", id, status, err, want)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	if resumed, err := client.Resume(ctx, session.ID); err != nil || resumed != interrupted {
		t.Fatalf("resume=%s, %v", resumed, err)
	}

	for _, id := range []string{interrupted, queued} {
		if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
			t.Fatalf("resumed outcome=%s, %v", status, err)
		}
	}

	if err := os.Remove(filepath.Join(root, "release")); err != nil {
		t.Fatal(err)
	}

	id, err = client.Execute(ctx, session.ID, kit.NewTextContent("wait"))
	if err != nil {
		t.Fatal(err)
	}

	if err := client.Cancel(ctx, session.ID, id); err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Cancelled {
		t.Fatalf("cancel=%s, %v", status, err)
	}

	id, err = client.Execute(ctx, session.ID, kit.NewTextContent("wait"))
	if err != nil {
		t.Fatal(err)
	}

	health, err = manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}

	process, err := os.FindProcess(health.PID)
	if err != nil {
		t.Fatal(err)
	}

	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}

	for {
		status, err := manager.Status(ctx)
		if err == nil && status.PID == 0 {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	client.Close()

	if err := manager.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	client = api.NewClient(manager.SocketPath(), root)
	defer client.Close()

	if status, err := client.GetExecutionStatus(ctx, session.ID, id); err != nil || status != execution.Interrupted {
		t.Fatalf("after crash=%s, %v", status, err)
	}

	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Resume(ctx, session.ID); err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("crash recovery=%s, %v", status, err)
	}
}

func TestManagerStartupFailurePointsToLog(t *testing.T) {
	root := newSocketRoot(t)
	manager := NewManager(filepath.Join(root, "runtime"), []string{"/bin/sh", "-c", "echo startup-failed >&2; exit 1"}, os.Environ())

	err := manager.Ensure(t.Context())
	if err == nil || !strings.Contains(err.Error(), manager.LogPath()) {
		t.Fatalf("startup error: %v", err)
	}

	log, err := os.ReadFile(manager.LogPath())
	if err != nil || !strings.Contains(string(log), "startup-failed") {
		t.Fatalf("log=%q, %v", log, err)
	}
}
