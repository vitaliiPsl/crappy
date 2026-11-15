package schedule

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestToolsPersistAndScopeSchedules(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "schedules.json")

	manager, err := NewManager(t.Context(), NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}

	tools := NewTools(manager, "first")

	created := callTool[Record](t, tools, "schedule_create", map[string]any{
		"instruction": "write a report", "at": "2027-01-02T09:30:00+08:00",
	})
	if created.State != Scheduled || created.SessionID != "first" || created.At.Format(time.RFC3339) != "2027-01-02T01:30:00Z" {
		t.Fatalf("created = %+v", created)
	}

	restored, err := NewManager(t.Context(), NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}

	tools = NewTools(restored, "first")

	listed := callTool[[]Record](t, tools, "schedule_list", map[string]any{})
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Instruction != created.Instruction {
		t.Fatalf("restored schedules = %+v", listed)
	}

	other := NewTools(restored, "second")

	_, err = findTool(t, other, "schedule_cancel").Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", "schedule_cancel", map[string]any{"id": created.ID}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancellation crossed session boundary: %v", err)
	}

	if records := callTool[[]Record](t, other, "schedule_list", map[string]any{}); len(records) != 0 {
		t.Fatalf("other session saw schedules: %+v", records)
	}

	cancelled := callTool[Record](t, tools, "schedule_cancel", map[string]any{"id": created.ID})
	if cancelled.State != Cancelled {
		t.Fatalf("cancellation = %+v", cancelled)
	}

	restored, err = NewManager(t.Context(), NewJSONStore(path))
	if err != nil {
		t.Fatal(err)
	}

	if record, err := restored.Cancel(t.Context(), "first", created.ID); err != nil || record.State != Cancelled {
		t.Fatalf("repeated cancellation after restart = %+v, %v", record, err)
	}
}

func TestCreateToolRequiresTimestamp(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(t.Context(), NewJSONStore(filepath.Join(t.TempDir(), "schedules.json")))
	if err != nil {
		t.Fatal(err)
	}

	tool := findTool(t, NewTools(manager, "session"), "schedule_create")

	for _, at := range []string{"", "invalid", "2027-01-02T09:30:00"} {
		_, err := tool.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", "schedule_create", map[string]any{
			"instruction": "write a report", "at": at,
		}))
		if err == nil {
			t.Fatalf("accepted timestamp %q", at)
		}
	}
}

func findTool(t *testing.T, tools []kit.Tool, name string) kit.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool
		}
	}

	t.Fatalf("tool %s not found", name)

	return nil
}

func callTool[T any](t *testing.T, tools []kit.Tool, name string, args map[string]any) T {
	t.Helper()

	output, err := findTool(t, tools, name).Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", name, args))
	if err != nil {
		t.Fatal(err)
	}

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
