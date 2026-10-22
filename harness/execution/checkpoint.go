package execution

import (
	"context"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/memory"
)

func (a *Agent) checkpoint(
	ctx context.Context,
	params ExecuteParams,
	agentMemory *memory.Memory,
) (*checkpoint.Checkpoint, error) {
	previous, messages, err := agentMemory.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint context: %w", err)
	}

	if err := params.Events.Emit(ctx, params.ExecutionID, checkpoint.StartedEventType, nil); err != nil {
		return nil, err
	}

	generated, err := a.checkpoints.Generate(ctx, previous, messages)
	if err != nil {
		return nil, err
	}

	if err := params.Events.Commit(ctx, params.ExecutionID, checkpoint.EntryType, generated); err != nil {
		return nil, fmt.Errorf("commit checkpoint: %w", err)
	}

	return generated, nil
}
