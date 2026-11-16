package schedule

import (
	"fmt"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

type createInput struct {
	Instruction string    `json:"instruction" jsonschema:"Instruction to execute at the scheduled time"`
	At          time.Time `json:"at,omitzero" jsonschema:"RFC3339 timestamp with UTC offset for a one-time schedule"`
	Cron        string    `json:"cron,omitempty" jsonschema:"Five-field cron expression in local time for a recurring schedule, such as 0 9 * * 1-5"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Schedule ID"`
}

func NewTools(manager *Manager, sessionID string) []kit.Tool {
	return []kit.Tool{
		tool.MustNew("schedule_create", "Schedule an instruction to run later in this conversation, once at a timestamp or repeatedly on a cron expression.",
			func(run *kit.RunContext, input createInput) (Record, error) {
				if input.Cron == "" {
					return manager.Create(run.Context, sessionID, input.Instruction, input.At)
				}

				if !input.At.IsZero() {
					return Record{}, fmt.Errorf("%w: pass either at or cron, not both", ErrInvalid)
				}

				return manager.CreateRecurring(run.Context, sessionID, input.Instruction, input.Cron)
			}),
		tool.MustNew("schedule_list", "List this conversation's schedules.",
			func(run *kit.RunContext, _ struct{}) ([]Record, error) {
				return manager.List(run.Context, sessionID)
			}),
		tool.MustNew("schedule_cancel", "Cancel a schedule before dispatch starts, ending a recurring schedule.",
			func(run *kit.RunContext, input idInput) (Record, error) {
				return manager.Cancel(run.Context, sessionID, input.ID)
			}),
	}
}
