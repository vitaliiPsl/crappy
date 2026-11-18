package queue

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestQueueViewLabelsInterruptedWorkAndFitsSize(t *testing.T) {
	input, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent("write\nthe \x1b[31mweekly\x1b[0m report " + strings.Repeat("long ", 20))))
	if err != nil {
		t.Fatal(err)
	}

	blank, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(" \n\t")))
	if err != nil {
		t.Fatal(err)
	}

	model := New()
	model.SetSize(40, 6)
	model.SetEntries([]execution.Record{
		{ID: "first", Executor: agent.ExecutorName, Input: input, Status: execution.Interrupted},
		{ID: "second", Executor: compactionexec.ExecutorName, Status: execution.Queued},
		{ID: "third", Executor: agent.ExecutorName, Input: blank, Status: execution.Queued},
	})

	view := model.View()
	if width, height := lipgloss.Width(view), lipgloss.Height(view); width != 40 || height != 6 {
		t.Fatalf("view = %dx%d, want 40x6: %q", width, height, view)
	}

	lines := strings.Split(ansi.Strip(view), "\n")
	for i, want := range []string{
		"› Interrupted  write the weekly report …",
		"  1            /compact",
		"  2            Message",
	} {
		if got := strings.TrimRight(lines[i+2], " "); got != want {
			t.Errorf("line %d = %q, want %q", i+2, got, want)
		}
	}
}

func TestQueueViewShowsEmptyQueueWithoutCancel(t *testing.T) {
	model := New()

	if view := ansi.Strip(model.View()); !strings.Contains(view, "Pending work") || !strings.Contains(view, "No pending work") {
		t.Fatalf("empty view = %q", view)
	}

	if shortcuts := ansi.Strip(model.Shortcuts(true)); strings.Contains(shortcuts, "Cancel") {
		t.Fatalf("empty queue offers cancel: %q", shortcuts)
	}

	model.SetEntries(records("first"))

	if shortcuts := ansi.Strip(model.Shortcuts(true)); !strings.Contains(shortcuts, "Cancel") {
		t.Fatalf("queue does not offer cancel: %q", shortcuts)
	}

	if shortcuts := ansi.Strip(model.Shortcuts(false)); strings.Contains(shortcuts, "Cancel") {
		t.Fatalf("closed session offers cancel: %q", shortcuts)
	}
}
