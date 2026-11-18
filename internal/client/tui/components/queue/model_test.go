package queue

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestQueueKeepsSelectionByIDWhenEntriesChange(t *testing.T) {
	model := New()
	model.SetEntries(records("first", "second", "third"))
	model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	model.SetEntries(records("first", "third"))

	if selected := model.Selected().ID; selected != "third" {
		t.Fatalf("selected = %q, want third", selected)
	}

	model.SetEntries(records("first"))

	if selected := model.Selected().ID; selected != "first" {
		t.Fatalf("selected = %q, want first", selected)
	}
}

func TestQueueKeepsSelectionVisible(t *testing.T) {
	model := New()
	model.SetSize(40, 5)
	model.SetEntries(records("0", "1", "2", "3", "4", "5", "6", "7", "8", "9"))

	model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})

	if model.Selected().ID != "9" || model.offset != 7 {
		t.Fatalf("selected = %q, offset = %d", model.Selected().ID, model.offset)
	}

	model.SetSize(40, 16)

	if model.offset != 0 {
		t.Fatalf("offset after growing = %d", model.offset)
	}

	model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model.SetSize(40, 3)

	if model.Selected().ID != "8" || model.offset != 8 {
		t.Fatalf("selected = %q, offset = %d", model.Selected().ID, model.offset)
	}
}

func TestQueueCancelsSelectedEntry(t *testing.T) {
	model := New()
	if command := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}); command != nil {
		t.Fatal("empty queue emitted a cancellation")
	}

	model.SetEntries(records("first", "second"))
	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	command := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cancel := command().(Cancel); cancel.ExecutionID != "second" {
		t.Fatalf("cancel = %+v", cancel)
	}
}

func TestQueueClosesOnEscape(t *testing.T) {
	model := New()
	model.Open()
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if model.Opened() {
		t.Fatal("queue stayed open")
	}
}

func records(ids ...string) []execution.Record {
	result := make([]execution.Record, 0, len(ids))
	for _, id := range ids {
		result = append(result, execution.Record{ID: id, Status: execution.Queued})
	}

	return result
}
