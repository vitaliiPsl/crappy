package compaction

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestCompactionMessageRendersSections(t *testing.T) {
	t.Parallel()

	message := Compaction{Summary: Summary{
		Objective: "ship compaction",
		Progress:  []string{"done: retained tail"},
		Next:      []string{"evaluate compactions"},
	}}.Message()

	want := messagePreamble + `

<compaction>
## Objective
ship compaction

## Progress
- done: retained tail

## Next
- evaluate compactions
</compaction>`

	if got := message.TextContent().Text; got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
}

func TestCompactionPersistsSummaryAndRetainedMessages(t *testing.T) {
	t.Parallel()
	compaction := Compaction{
		Summary:  Summary{Objective: "ship compaction", Progress: []string{"done: research"}},
		Retained: []kit.Message{kit.NewUserMessage(kit.NewTextContent("continue"))},
	}
	payload, err := json.Marshal(compaction)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, found := fields["messages"]; found {
		t.Fatal("compaction persisted a second copy of the model context")
	}
	var restored Compaction
	if err := json.Unmarshal(payload, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, compaction) {
		t.Fatalf("compaction changed after persistence: %+v", restored)
	}
}
