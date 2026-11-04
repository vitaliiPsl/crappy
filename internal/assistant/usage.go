package assistant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/event"
)

func (a *Assistant) Usage(ctx context.Context, sessionID string) (kit.Usage, error) {
	if _, err := a.GetSession(ctx, sessionID); err != nil {
		return kit.Usage{}, err
	}

	entries, err := a.entries.List(ctx, sessionID, 0)
	if err != nil {
		return kit.Usage{}, err
	}

	var total kit.Usage
	for _, stored := range entries {
		usage, err := UsageFromEvent(event.FromEntry(stored))
		if err != nil {
			return kit.Usage{}, err
		}

		total.Add(usage)
	}

	return total, nil
}

func UsageFromEvent(value event.Event) (kit.Usage, error) {
	switch value.Type {
	case agent.MessageEntryType, compaction.EntryType:
		var measured struct {
			Usage kit.Usage `json:"usage"`
		}
		if err := json.Unmarshal(value.Payload, &measured); err != nil {
			return kit.Usage{}, fmt.Errorf("decode %s usage: %w", value.Type, err)
		}

		return measured.Usage, nil
	default:
		return kit.Usage{}, nil
	}
}
