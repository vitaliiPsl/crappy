package session

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/execution"
)

func (s *Session) restore(ctx context.Context) error {
	entries, err := s.entries.List(ctx, s.id, 0)
	if err != nil {
		return err
	}

	latest := make(map[string]execution.Record)

	var order []string

	for _, value := range entries {
		if value.Type != execution.EntryType {
			continue
		}

		var record execution.Record
		if err := json.Unmarshal(value.Payload, &record); err != nil {
			return fmt.Errorf("decode execution %s: %w", value.ExecutionID, err)
		}

		if _, seen := latest[record.ID]; !seen {
			order = append(order, record.ID)
		}

		latest[record.ID] = record
	}

	for _, id := range order {
		if record := latest[id]; !record.Status.Terminal() {
			if err := s.commitExecution(ctx, newExecution(record), execution.Interrupted, ""); err != nil {
				return err
			}
		}
	}

	return nil
}
