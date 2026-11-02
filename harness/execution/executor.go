package execution

import (
	"context"
	"encoding/json"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type ExecuteParams struct {
	SessionID   string
	ExecutionID string

	Input json.RawMessage

	State   state.State
	Entries entry.Store
	Events  event.Publisher
}

type Executor interface {
	Execute(context.Context, ExecuteParams) error
}

type Resolver interface {
	Resolve(name string) (Executor, error)
}

type Func func(context.Context, ExecuteParams) error

func (f Func) Execute(ctx context.Context, params ExecuteParams) error {
	return f(ctx, params)
}
