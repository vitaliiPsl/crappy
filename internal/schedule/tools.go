package schedule

import (
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

type createInput struct {
	Instruction string    `json:"instruction" jsonschema:"Instruction to execute at the scheduled time"`
	At          time.Time `json:"at" jsonschema:"RFC3339 timestamp with UTC offset"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Schedule ID"`
}

func NewTools(manager *Manager, sessionID string) []kit.Tool {
	return []kit.Tool{
		tool.MustNew("schedule_create", "Schedule one instruction to run later in this conversation.",
			func(run *kit.RunContext, input createInput) (Record, error) {
				return manager.Create(run.Context, sessionID, input.Instruction, input.At)
			}),
		tool.MustNew("schedule_list", "List this conversation's schedules.",
			func(run *kit.RunContext, _ struct{}) ([]Record, error) {
				return manager.List(run.Context, sessionID)
			}),
		tool.MustNew("schedule_cancel", "Cancel a schedule before dispatch starts.",
			func(run *kit.RunContext, input idInput) (Record, error) {
				return manager.Cancel(run.Context, sessionID, input.ID)
			}),
	}
}
