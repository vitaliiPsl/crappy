package execution

import (
	"context"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func (e *Executor) checkpoint(
	ctx context.Context,
	executionID string,
	agentMemory *memory.Memory,
) (*checkpoint.Checkpoint, error) {
	previous, messages, err := agentMemory.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint context: %w", err)
	}

	if err := e.events.Emit(ctx, executionID, checkpoint.StartedEventType, nil); err != nil {
		return nil, err
	}

	generated, err := e.checkpoints.Generate(ctx, previous, messages)
	if err != nil {
		return nil, err
	}

	if err := e.events.Commit(ctx, executionID, checkpoint.EntryType, generated); err != nil {
		return nil, fmt.Errorf("commit checkpoint: %w", err)
	}

	return generated, nil
}
