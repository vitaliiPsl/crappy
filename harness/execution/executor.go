package execution

import (
	"context"
	"encoding/json"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

const DefaultName = "agent"

type ExecuteParams struct {
	SessionID   string
	ExecutionID string

	Input json.RawMessage

	State   state.State
	Entries entry.Store
	Events  event.Publisher
}

type Executor interface {
	Execute(context.Context, ExecuteParams) (any, error)
}

type Resolver interface {
	Resolve(name string) (Executor, error)
}

type Func func(context.Context, ExecuteParams) (any, error)

func (f Func) Execute(ctx context.Context, params ExecuteParams) (any, error) {
	return f(ctx, params)
}
