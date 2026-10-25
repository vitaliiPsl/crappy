package checkpoint

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestCheckpointMessageRendersSections(t *testing.T) {
	t.Parallel()

	message := Checkpoint{Summary: Summary{
		Objective: "ship compaction",
		Progress:  []string{"done: retained tail"},
		Next:      []string{"evaluate checkpoints"},
	}}.Message()

	want := messagePreamble + `

<checkpoint>
## Objective
ship compaction

## Progress
- done: retained tail

## Next
- evaluate checkpoints
</checkpoint>`

	if got := message.TextContent().Text; got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
}

func TestCheckpointPersistsSummaryAndRetainedMessages(t *testing.T) {
	t.Parallel()
	checkpoint := Checkpoint{
		Summary:  Summary{Objective: "ship compaction", Progress: []string{"done: research"}},
		Retained: []kit.Message{kit.NewUserMessage(kit.NewTextContent("continue"))},
	}
	payload, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, found := fields["messages"]; found {
		t.Fatal("checkpoint persisted a second copy of the model context")
	}
	var restored Checkpoint
	if err := json.Unmarshal(payload, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, checkpoint) {
		t.Fatalf("checkpoint changed after persistence: %+v", restored)
	}
}
