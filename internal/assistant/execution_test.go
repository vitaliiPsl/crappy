package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/internal/config"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestExecuteAndCancelUseHarnessExecutionLifetime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan context.Context, 1)
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName,
		execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
			var input kit.Message
			if err := json.Unmarshal(params.Input, &input); err != nil {
				return err
			}

			if input.Role != kit.RoleUser || input.TextContent().Text != "hello" {
				t.Errorf("agent input = %+v", input)
			}

			started <- ctx

			<-ctx.Done()

			return ctx.Err()
		})))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	created, err := app.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	observer, err := app.Subscribe(ctx, created.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	caller, disconnect := context.WithCancel(ctx)

	current, err := app.Execute(caller, created.ID, kit.NewTextContent("hello"))
	if err != nil {
		t.Fatal(err)
	}

	var running context.Context
	select {
	case running = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	disconnect()
	observer.Close()

	if err := running.Err(); err != nil {
		t.Fatalf("disconnecting the caller stopped work: %v", err)
	}

	if _, err := app.Execute(ctx, created.ID, kit.NewTextContent("second")); !errors.Is(err, session.ErrBusy) {
		t.Fatalf("concurrent send error = %v", err)
	}

	if err := app.Cancel(ctx, created.ID, current.ID()); err != nil {
		t.Fatal(err)
	}

	finished, err := current.Wait(ctx)
	if err != nil || finished.Status != execution.Cancelled {
		t.Fatalf("finish = %+v, error = %v", finished, err)
	}

	loaded, err := app.Execution(ctx, created.ID, current.ID())
	if err != nil {
		t.Fatal(err)
	}

	stored, err := loaded.Wait(ctx)
	if err != nil || stored.Status != execution.Cancelled {
		t.Fatalf("persisted finish = %+v, error = %v", stored, err)
	}

	if err := app.Cancel(ctx, created.ID, current.ID()); err != nil {
		t.Fatalf("cancelling settled work = %v", err)
	}
}

func TestOperationsRequireAnExistingSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	operations := []func() error{
		func() error {
			_, err := app.Execute(ctx, "", kit.NewTextContent("hello"))

			return err
		},
		func() error { return app.Cancel(ctx, "", "execution") },
		func() error {
			_, err := app.Execution(ctx, "", "execution")

			return err
		},
		func() error {
			_, err := app.Subscribe(ctx, "", 0)

			return err
		},
		func() error {
			_, err := app.Compact(ctx, "")

			return err
		},
	}
	for _, operation := range operations {
		if err := operation(); !errors.Is(err, ErrSessionRequired) {
			t.Fatalf("operation error = %v", err)
		}
	}

	if records, err := app.ListSessions(ctx); err != nil || len(records) != 0 {
		t.Fatalf("operations created sessions: %+v, %v", records, err)
	}
}
