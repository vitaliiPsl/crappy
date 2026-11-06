package executor

import (
	"context"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/execution"
)

const ExecutorName = "compaction"

var _ execution.Executor = (*Executor)(nil)

type Executor struct {
	compactor *compaction.Compactor
}

func New(compactor *compaction.Compactor) *Executor {
	return &Executor{
		compactor: compactor,
	}
}

func (e *Executor) Execute(ctx context.Context, params execution.ExecuteParams) error {
	latest, found, err := params.Entries.Latest(ctx, params.SessionID, compaction.EntryType)
	if err != nil {
		return fmt.Errorf("read compaction history: %w", err)
	}

	if found && latest.ExecutionID == params.ExecutionID {
		return nil
	}

	_, err = agent.NewHistory(params.SessionID, params.ExecutionID, params.Entries, params.Events).
		Compact(ctx, e.compactor)

	return err
}
