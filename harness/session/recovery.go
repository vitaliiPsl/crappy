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
		record := latest[id]
		if record.Status.Terminal() {
			continue
		}

		if record.Status == execution.Running {
			record.Status, record.Error = execution.Interrupted, ""
			if err := s.Commit(ctx, record.ID, execution.EntryType, record); err != nil {
				return fmt.Errorf("recover execution %s: %w", record.ID, err)
			}
		}

		pending := newExecution(record)
		if s.current == nil {
			s.current = pending
		} else {
			s.queue = append(s.queue, pending)
		}
	}

	return nil
}
