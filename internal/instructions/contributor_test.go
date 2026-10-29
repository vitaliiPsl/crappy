package instructions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestContributorLoadsAncestorsInOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "project")

	files := []struct {
		directory string
		name      string
		content   string
	}{
		{root, "AGENTS.md", "parent agents"},
		{root, "CLAUDE.md", "parent claude"},
		{directory, "AGENTS.md", "project agents"},
		{directory, "CLAUDE.md", "project claude"},
	}
	for _, file := range files {
		writeInstruction(t, file.directory, file.name, file.content)
	}

	writeInstruction(t, filepath.Join(root, "sibling"), "AGENTS.md", "sibling instructions")
	writeInstruction(t, filepath.Join(directory, "nested"), "AGENTS.md", "nested instructions")

	contribution, err := NewContributor().Contribute(context.Background(), buildContext(t, directory))
	if err != nil {
		t.Fatal(err)
	}

	if len(contribution.Instructions) != 1 {
		t.Fatalf("instructions = %v, want one section", contribution.Instructions)
	}

	instructions := contribution.Instructions[0]
	previous := -1

	for _, file := range files {
		section := "## " + filepath.Join(file.directory, file.name) + "\n\n" + file.content

		position := strings.Index(instructions, section)
		if position <= previous {
			t.Fatalf("missing or out-of-order section %q: %q", section, instructions)
		}

		previous = position
	}

	if strings.Contains(instructions, "sibling instructions") || strings.Contains(instructions, "nested instructions") {
		t.Fatalf("instructions include files outside the ancestor chain: %q", instructions)
	}
}

func TestContributorRefreshesFilesAndWorkdirBetweenExecutions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	writeInstruction(t, directory, "AGENTS.md", "first version")
	build := buildContext(t, directory)
	contributor := NewContributor()

	first, err := contributor.Contribute(ctx, build)
	if err != nil {
		t.Fatal(err)
	}

	repeated, err := contributor.Contribute(ctx, build)
	if err != nil {
		t.Fatal(err)
	}

	if first.Instructions[0] != repeated.Instructions[0] {
		t.Fatal("unchanged files produced different instructions")
	}

	writeInstruction(t, directory, "AGENTS.md", "second version")

	updated, err := contributor.Contribute(ctx, build)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(updated.Instructions[0], "first version") || !strings.Contains(updated.Instructions[0], "second version") {
		t.Fatalf("instructions did not refresh: %q", updated.Instructions[0])
	}

	other := t.TempDir()
	writeInstruction(t, other, "CLAUDE.md", "other directory")

	if err := build.Request.State.Set(ctx, workdir.StateKey, other); err != nil {
		t.Fatal(err)
	}

	moved, err := contributor.Contribute(ctx, build)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(moved.Instructions[0], "second version") || !strings.Contains(moved.Instructions[0], "other directory") {
		t.Fatalf("instructions used the previous workdir: %q", moved.Instructions[0])
	}

	if !strings.Contains(first.Instructions[0], "first version") {
		t.Fatal("a later build changed an earlier contribution")
	}
}

func TestContributorSkipsMissingEmptyAndDirectoryEntries(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeInstruction(t, directory, "AGENTS.md", " \n\t ")

	if err := os.Mkdir(filepath.Join(directory, "CLAUDE.md"), 0o700); err != nil {
		t.Fatal(err)
	}

	contribution, err := NewContributor().Contribute(context.Background(), buildContext(t, directory))
	if err != nil {
		t.Fatal(err)
	}

	if len(contribution.Instructions) != 0 {
		t.Fatalf("empty instruction sources produced a section: %v", contribution.Instructions)
	}
}

func TestContributorSkipsInstructionsOverLimits(t *testing.T) {
	t.Parallel()

	for _, scope := range []string{"file", "total"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			directory := root
			content := strings.Repeat("x", maxInstructionFileBytes)
			writeInstruction(t, root, "AGENTS.md", content)
			writeInstruction(t, root, "CLAUDE.md", "retained instructions")

			if scope == "total" {
				directory = filepath.Join(root, "project", "nested")
				writeInstruction(t, root, "CLAUDE.md", content)
				writeInstruction(t, filepath.Dir(directory), "AGENTS.md", content)
				writeInstruction(t, filepath.Dir(directory), "CLAUDE.md", content)

				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			ctx := context.Background()
			build := buildContext(t, directory)

			contributor := NewContributor()
			if _, err := contributor.Contribute(ctx, build); err != nil {
				t.Fatalf("instructions at the %s limit were rejected: %v", scope, err)
			}

			if scope == "file" {
				content += "x"
			} else {
				content = "x"
			}

			writeInstruction(t, directory, "AGENTS.md", content)

			contribution, err := contributor.Contribute(ctx, build)
			if err != nil {
				t.Fatalf("oversized %s instructions stopped the contribution: %v", scope, err)
			}

			if len(contribution.Instructions) != 1 {
				t.Fatalf("valid instruction files were discarded: %v", contribution.Instructions)
			}

			instructions := contribution.Instructions[0]
			if strings.Contains(instructions, "## "+filepath.Join(directory, "AGENTS.md")) {
				t.Fatal("oversized instruction file was included")
			}

			if !strings.Contains(instructions, "## "+filepath.Join(root, "CLAUDE.md")) {
				t.Fatal("valid instruction file was skipped")
			}
		})
	}
}

func buildContext(t *testing.T, directory string) agent.BuildContext {
	t.Helper()

	ctx := context.Background()

	store := state.NewMemoryStore()
	if err := store.Save(ctx, state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	sessionState := state.New(store, "session")
	if err := sessionState.Set(ctx, workdir.StateKey, directory); err != nil {
		t.Fatal(err)
	}

	return agent.BuildContext{Request: agent.BuildRequest{State: sessionState}}
}

func writeInstruction(t *testing.T, directory, name, content string) {
	t.Helper()

	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
