package executor

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
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
	generated, compacted, err := agent.NewHistory(params.SessionID, params.ExecutionID, params.Entries, params.Events).
		Compact(ctx, e.compactor)

	return Result{
		Compacted:        compacted,
		TokensBefore:     generated.TokensBefore,
		TokensAfter:      generated.TokensAfter,
		RetainedMessages: len(generated.Retained),
		Usage:            generated.Usage,
	}, err
}
