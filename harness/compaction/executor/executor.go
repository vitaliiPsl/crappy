package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
)

const ExecutorName = "compaction"

type Result struct {
	Compacted        bool      `json:"compacted"`
	TokensBefore     int       `json:"tokens_before"`
	TokensAfter      int       `json:"tokens_after"`
	RetainedMessages int       `json:"retained_messages"`
	Usage            kit.Usage `json:"usage"`
}

var _ execution.Executor = (*Executor)(nil)

type Executor struct {
	compactor *compaction.Compactor
}

func New(compactor *compaction.Compactor) *Executor {
	return &Executor{
		compactor: compactor,
	}
}

func (e *Executor) Execute(ctx context.Context, params execution.ExecuteParams) (any, error) {
	previous, messages, err := loadCompactionContext(ctx, params.SessionID, params.Entries)
	if err != nil {
		return Result{}, fmt.Errorf("read compaction context: %w", err)
	}

	if previous == nil && len(messages) == 0 {
		return Result{}, nil
	}

	if err := params.Events.Emit(ctx, params.ExecutionID, compaction.StartedEventType, nil); err != nil {
		return Result{}, fmt.Errorf("start compaction: %w", err)
	}

	generated, err := e.compactor.Compact(ctx, previous, messages)
	if err != nil {
		return Result{Usage: generated.Usage}, err
	}

	result := Result{
		TokensBefore:     generated.TokensBefore,
		TokensAfter:      generated.TokensAfter,
		RetainedMessages: len(generated.Retained),
		Usage:            generated.Usage,
	}

	if err := params.Events.Commit(ctx, params.ExecutionID, compaction.EntryType, generated); err != nil {
		return result, fmt.Errorf("commit compaction: %w", err)
	}

	result.Compacted = true

	return result, nil
}

func loadCompactionContext(ctx context.Context, sessionID string, store entry.Store) (*compaction.Compaction, []kit.Message, error) {
	latest, found, err := store.Latest(ctx, sessionID, compaction.EntryType)
	if err != nil {
		return nil, nil, err
	}

	var (
		previous *compaction.Compaction
		messages []kit.Message
		afterID  uint64
	)

	if found {
		previous = &compaction.Compaction{}
		if err := json.Unmarshal(latest.Payload, previous); err != nil {
			return nil, nil, fmt.Errorf("decode compaction entry %d: %w", latest.ID, err)
		}

		messages = append(messages, previous.Retained...)
		afterID = latest.ID
	}

	records, err := store.List(ctx, sessionID, afterID)
	if err != nil {
		return nil, nil, err
	}

	for _, record := range records {
		if record.Type != agent.MessageEntryType {
			continue
		}

		var message kit.Message

		if err := json.Unmarshal(record.Payload, &message); err != nil {
			return nil, nil, fmt.Errorf("decode message entry %d: %w", record.ID, err)
		}

		messages = append(messages, message)
	}

	return previous, messages, nil
}
