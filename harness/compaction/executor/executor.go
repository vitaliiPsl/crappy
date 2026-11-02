package executor

import (
	"context"

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
	_, err := agent.NewHistory(params.SessionID, params.ExecutionID, params.Entries, params.Events).
		Compact(ctx, e.compactor)

	return err
}
