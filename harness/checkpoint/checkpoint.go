package checkpoint

import (
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const EntryType = "checkpoint"

const messageFormat = `The conversation before this point was compacted into the following checkpoint. The messages after it are kept verbatim.

<checkpoint>
%s
</checkpoint>`

type Checkpoint struct {
	Summary  string    `json:"summary"`
	Retained int       `json:"retained"`
	Usage    kit.Usage `json:"usage"`
}

func (c Checkpoint) Message() kit.Message {
	return kit.NewUserMessage(kit.NewTextContent(fmt.Sprintf(messageFormat, c.Summary)))
}
