package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestContributorBindsToolsToSessionAndExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := state.NewMemoryStore()
	first := newSessionState(t, store, "first")
	second := newSessionState(t, store, "second")
	contributor := NewContributor()

	for _, test := range []struct {
		name    string
		state   state.State
		workdir string
	}{
		{name: "first execution", state: first, workdir: filepath.Join(t.TempDir(), `quoted "目录"`)},
		{name: "other session", state: second, workdir: t.TempDir()},
		{name: "next execution", state: first, workdir: t.TempDir()},
	} {
		if err := os.MkdirAll(test.workdir, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(test.workdir, "marker"), []byte(test.workdir), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := test.state.Set(ctx, workdir.StateKey, test.workdir); err != nil {
			t.Fatal(err)
		}

		contribution := contributeForSession(t, contributor, test.state)
		if len(contribution.Instructions) != 1 || !strings.Contains(contribution.Instructions[0], fmt.Sprintf("Working directory: %q", test.workdir)) {
			t.Fatalf("instructions = %v, want workdir %q", contribution.Instructions, test.workdir)
		}

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, call := range []kit.ToolCall{
				kit.NewToolCall("bash", "bash", map[string]any{"command": "cat marker", "description": "Read session marker"}),
				kit.NewToolCall("read", "read_file", map[string]any{"path": "marker"}),
			} {
				if got := executeContributedTool(t, contribution, call); got != test.workdir {
					t.Fatalf("%s used another workdir: got %q, want %q", call.Name, got, test.workdir)
				}
			}
		})
	}
}

func TestContributorBindsShellToExecution(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'custom:'\nexec /bin/sh \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		shell  string
		bound  string
		output string
	}{
		{name: "configured", shell: shell, bound: shell, output: "custom:hello"},
		{name: "default", shell: "", bound: "sh", output: "hello"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SHELL", test.shell)

			ctx := context.Background()

			sessionState := newSessionState(t, state.NewMemoryStore(), "session")
			if err := sessionState.Set(ctx, workdir.StateKey, t.TempDir()); err != nil {
				t.Fatal(err)
			}

			contributor := NewContributor()

			first := contributeForSession(t, contributor, sessionState)
			for _, want := range []string{
				fmt.Sprintf("Shell: %q", test.bound),
				"Platform: " + runtime.GOOS,
				"Timezone: ",
			} {
				if !strings.Contains(first.Instructions[0], want) {
					t.Fatalf("instructions are missing %q: %q", want, first.Instructions[0])
				}
			}

			t.Setenv("SHELL", "/bin/sh")
			next := contributeForSession(t, contributor, sessionState)
			call := kit.NewToolCall("bash", "bash", map[string]any{
				"command": "printf hello", "description": "Check the selected shell",
			})

			if got := executeContributedTool(t, first, call); got != test.output {
				t.Fatalf("first execution output = %q, want %q", got, test.output)
			}

			if got := executeContributedTool(t, next, call); got != "hello" {
				t.Fatalf("next execution output = %q, want hello", got)
			}

			if !strings.Contains(next.Instructions[0], `Shell: "/bin/sh"`) {
				t.Fatalf("next execution has stale shell instructions: %q", next.Instructions[0])
			}
		})
	}
}

func newSessionState(t *testing.T, store state.Store, id string) state.State {
	t.Helper()

	if err := store.Save(context.Background(), state.Record{ID: id}); err != nil {
		t.Fatal(err)
	}

	return state.New(store, id)
}

func contributeForSession(t *testing.T, contributor *Contributor, sessionState state.State) agent.Contribution {
	t.Helper()

	contribution, err := contributor.Contribute(context.Background(), agent.BuildContext{
		Request: agent.BuildRequest{State: sessionState},
	})
	if err != nil {
		t.Fatal(err)
	}

	return contribution
}

func executeContributedTool(t *testing.T, contribution agent.Contribution, call kit.ToolCall) string {
	t.Helper()

	for _, tool := range contribution.Tools {
		if tool.Definition().Name != call.Name {
			continue
		}

		output, err := tool.Execute(kit.NewRunContext(context.Background()), call)
		if err != nil {
			t.Fatal(err)
		}

		if len(output.Content) != 1 || output.Content[0].Text == nil {
			t.Fatalf("%s output = %+v, want text", call.Name, output)
		}

		return output.Content[0].Text.Text
	}

	t.Fatalf("tool %s was not contributed", call.Name)

	return ""
}
