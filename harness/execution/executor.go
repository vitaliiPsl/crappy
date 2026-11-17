package execution

import (
	"context"
	"encoding/json"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

type ExecuteParams struct {
	SessionID   string
	ExecutionID string

	Input json.RawMessage

	State        state.State
	Entries      entry.Store
	Events       event.Publisher
	Interactions interaction.Prompter

	// Steer removes inputs after accept succeeds. Call it synchronously
	// from Execute, with a single consumer, to keep delivery ordered with execution.
	Steer func(ctx context.Context, accept func(json.RawMessage) error) error
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
