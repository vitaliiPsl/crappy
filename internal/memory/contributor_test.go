package memory

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/workdir"
	corememory "github.com/vitaliipsl/crappy/memory"
)

func TestContributorUsesCurrentWorkdirWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.json")
	memory := openMemory(t, path)
	first, second := t.TempDir(), t.TempDir()

	personal, err := memory.Create(ctx, ScopePersonal, "Prefer concise answers")
	if err != nil {
		t.Fatal(err)
	}

	local, err := memory.Create(ctx, workdirScope(first), "First directory conventions")
	if err != nil {
		t.Fatal(err)
	}

	other, err := memory.Create(ctx, workdirScope(second), "Second directory conventions")
	if err != nil {
		t.Fatal(err)
	}

	store := state.NewMemoryStore()
	if err := store.Save(ctx, state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	sessionState := state.New(store, "session")
	contributor := NewContributor(memory)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var firstBuild agent.Contribution
	for _, test := range []struct {
		directory string
		present   string
		absent    string
	}{
		{first, local.ID, other.ID},
		{second, other.ID, local.ID},
	} {
		if err := sessionState.Set(ctx, workdir.StateKey, test.directory); err != nil {
			t.Fatal(err)
		}

		contribution, err := contributor.Contribute(ctx, agent.BuildContext{
			Request: agent.BuildRequest{SessionID: "session", State: sessionState},
		})
		if err != nil {
			t.Fatal(err)
		}

		if contribution.Source != "Memory" || len(contribution.Instructions) != 1 {
			t.Fatalf("contribution framing = %+v", contribution)
		}

		text := contribution.Instructions[0]
		if !strings.Contains(text, personal.ID) || !strings.Contains(text, test.present) || strings.Contains(text, test.absent) {
			t.Fatalf("catalog uses another workdir: %s", text)
		}

		if firstBuild.Tools == nil {
			firstBuild = contribution
		}
	}

	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("context construction changed memory: %s, %v", after, err)
	}

	page := decodeOutput[catalogPage](t, executeTool(t, firstBuild.Tools, "memory_list", map[string]any{"scope": ScopeWorkdir}))
	if len(page.Entries) != 1 || page.Entries[0].ID != local.ID {
		t.Fatalf("existing execution tools changed workdir: %+v", page)
	}

	results := decodeOutput[[]corememory.SearchResult](t, executeTool(t, firstBuild.Tools, "memory_search", map[string]any{"query": "conventions"}))
	if len(results) != 1 || results[0].Entry.ID != local.ID {
		t.Fatalf("existing search tool changed workdir: %+v", results)
	}
}
