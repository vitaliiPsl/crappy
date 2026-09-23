package checkpoint

import (
	"testing"
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
