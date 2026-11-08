package memory

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"

	corememory "github.com/vitaliipsl/crappy/memory"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
)

func TestToolsPersistRecallCorrectAndForget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(t.TempDir(), "memory.json")
	tools := NewTools(openMemory(t, path), directory, "session:first/execution:teach")

	created := decodeOutput[corememory.Entry](t, executeTool(t, tools, "memory_create", map[string]any{
		"scope": ScopeWorkdir, "content": "Run tests with go test ./...",
	}))
	if created.Scope != workdirScope(directory) || created.Version != 1 || created.Source != "session:first/execution:teach" {
		t.Fatalf("created entry = %+v", created)
	}

	reopened := openMemory(t, path)
	tools = NewTools(reopened, directory, "session:second/execution:correct")

	page := decodeOutput[catalogPage](t, executeTool(t, tools, "memory_list", map[string]any{"scope": ScopeWorkdir}))
	if len(page.Entries) != 1 || page.Entries[0].ID != created.ID {
		t.Fatalf("catalog after restart = %+v", page)
	}

	read := decodeOutput[corememory.Entry](t, executeTool(t, tools, "memory_get", map[string]any{"id": created.ID}))
	if read.Content != created.Content || read.Version != created.Version {
		t.Fatalf("recalled entry = %+v, want %+v", read, created)
	}

	updated := decodeOutput[corememory.Entry](t, executeTool(t, tools, "memory_update", map[string]any{
		"id": read.ID, "expected_version": read.Version, "content": "Run tests with go test -race ./...",
	}))
	if updated.Version != 2 || updated.Content == read.Content || updated.Source != "session:second/execution:correct" {
		t.Fatalf("corrected entry = %+v", updated)
	}

	for _, name := range []string{"memory_update", "memory_delete"} {
		args := map[string]any{"id": read.ID, "expected_version": read.Version}
		if name == "memory_update" {
			args["content"] = "stale content"
		}

		_, err := findTool(t, tools, name).Execute(kit.NewRunContext(ctx), kit.NewToolCall("stale", name, args))
		if !errors.Is(err, corememory.ErrConflict) {
			t.Fatalf("%s with stale version: %v", name, err)
		}
	}

	tools = NewTools(openMemory(t, path), directory, "session:third/execution:forget")

	read = decodeOutput[corememory.Entry](t, executeTool(t, tools, "memory_get", map[string]any{"id": updated.ID}))
	if read.Content != updated.Content || read.Version != updated.Version {
		t.Fatalf("correction did not survive restart: %+v", read)
	}

	executeTool(t, tools, "memory_delete", map[string]any{"id": read.ID, "expected_version": read.Version})

	if _, err := openMemory(t, path).Get(ctx, read.ID); !errors.Is(err, corememory.ErrNotFound) {
		t.Fatalf("forgotten entry after restart: %v", err)
	}
}

func TestToolsSelectNamespaces(t *testing.T) {
	t.Parallel()

	memory := openMemory(t, filepath.Join(t.TempDir(), "memory.json"))
	first := NewTools(memory, t.TempDir(), "first")

	second := NewTools(memory, t.TempDir(), "second")
	for _, test := range []struct {
		tools   []kit.Tool
		scope   string
		content string
	}{
		{first, ScopePersonal, "Prefer concise answers"},
		{first, ScopeWorkdir, "First directory conventions"},
		{second, ScopeWorkdir, "Second directory conventions"},
	} {
		executeTool(t, test.tools, "memory_create", map[string]any{"scope": test.scope, "content": test.content})
	}

	for _, test := range []struct {
		tools []kit.Tool
		want  string
	}{
		{first, "First directory conventions"},
		{second, "Second directory conventions"},
	} {
		page := decodeOutput[catalogPage](t, executeTool(t, test.tools, "memory_list", map[string]any{"scope": ScopeWorkdir}))
		if len(page.Entries) != 1 || page.Entries[0].Preview != test.want {
			t.Fatalf("workdir catalog = %+v, want %q", page, test.want)
		}

		page = decodeOutput[catalogPage](t, executeTool(t, test.tools, "memory_list", map[string]any{"scope": ScopePersonal}))
		if len(page.Entries) != 1 || page.Entries[0].Preview != "Prefer concise answers" {
			t.Fatalf("personal catalog = %+v", page)
		}
	}

	for _, name := range []string{"memory_list", "memory_create"} {
		args := map[string]any{"scope": "unknown"}
		if name == "memory_create" {
			args["content"] = "must not be saved"
		}

		_, err := findTool(t, first, name).Execute(kit.NewRunContext(context.Background()), kit.NewToolCall("invalid", name, args))
		if err == nil {
			t.Fatalf("%s accepted an unknown namespace", name)
		}
	}
}

func openMemory(t *testing.T, path string) *corememory.Memory {
	t.Helper()

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	return corememory.New(store)
}

func findTool(t *testing.T, tools []kit.Tool, name string) kit.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool
		}
	}

	t.Fatalf("tool %q was not contributed", name)

	return nil
}

func executeTool(t *testing.T, tools []kit.Tool, name string, args map[string]any) kit.ToolOutput {
	t.Helper()

	output, err := findTool(t, tools, name).Execute(kit.NewRunContext(context.Background()), kit.NewToolCall("call", name, args))
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func decodeOutput[T any](t *testing.T, output kit.ToolOutput) T {
	t.Helper()

	data, err := json.Marshal(output.Structured)
	if err != nil {
		t.Fatal(err)
	}

	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}

	return value
}
