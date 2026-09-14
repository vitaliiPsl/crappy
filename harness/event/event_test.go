package event

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestFromEntry(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"value":1}`)
	stored := entry.Entry{
		ID:          7,
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Type:        "test.entry",
		Payload:     payload,
	}

	event := FromEntry(stored)
	payload[0] = '['

	want := Event{
		EntryID:     7,
		SessionID:   "session-1",
		ExecutionID: "execution-1",
		Type:        "test.entry",
		Payload:     json.RawMessage(`{"value":1}`),
	}
	if !reflect.DeepEqual(event, want) {
		t.Fatalf("FromEntry() = %#v, want %#v", event, want)
	}
}

func TestTransientEventOmitsDurableFields(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Event{
		SessionID: "session-1",
		Type:      "agent.content_delta",
		Payload:   json.RawMessage(`{"type":"text","text":{"text":"hi"}}`),
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if _, ok := fields["entry_id"]; ok {
		t.Fatalf("encoded transient event contains entry_id: %s", encoded)
	}
}
