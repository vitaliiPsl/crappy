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

	started := make(chan context.Context, 2)
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName,
		execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
			var input kit.Message
			if err := json.Unmarshal(params.Input, &input); err != nil {
				return err
			}

			if input.Role != kit.RoleUser {
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

	id, err := app.Execute(caller, created.ID, kit.NewTextContent("hello"))
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

	active, err := app.GetExecutionStatus(ctx, created.ID, id)
	if err != nil || active != execution.Running {
		t.Fatalf("active execution status = %q, error = %v", active, err)
	}

	waiter, stopWaiting := context.WithCancel(ctx)
	stopWaiting()

	if _, err := app.WaitExecution(waiter, created.ID, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("disconnected observation = %v", err)
	}

	second, err := app.Execute(ctx, created.ID, kit.NewTextContent("second"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := app.GetExecutionStatus(ctx, created.ID, second); err != nil || status != execution.Queued {
		t.Fatalf("queued execution status = %q, %v", status, err)
	}

	if err := app.Cancel(ctx, created.ID, second); err != nil {
		t.Fatal(err)
	}

	if err := app.Cancel(ctx, created.ID, id); err != nil {
		t.Fatal(err)
	}

	status, err := app.WaitExecution(ctx, created.ID, id)
	if err != nil || status != execution.Cancelled {
		t.Fatalf("status = %q, error = %v", status, err)
	}

	loaded, err := app.GetExecutionStatus(ctx, created.ID, id)
	if err != nil || loaded != execution.Cancelled {
		t.Fatalf("persisted execution status = %q, error = %v", loaded, err)
	}

	stored, err := app.WaitExecution(ctx, created.ID, id)
	if err != nil || stored != execution.Cancelled {
		t.Fatalf("persisted finish = %+v, error = %v", stored, err)
	}

	if err := app.Cancel(ctx, created.ID, id); err != nil {
		t.Fatalf("cancelling settled work = %v", err)
	}
}

func TestExecutionObservationRestoresOutcomeAfterRestart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName,
		execution.Func(func(context.Context, execution.ExecuteParams) error {
			return errors.New("work failed")
		})))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	created, err := app.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	id, err := app.Execute(ctx, created.ID, kit.NewTextContent("hello"))
	if err != nil {
		t.Fatal(err)
	}

	status, err := app.WaitExecution(ctx, created.ID, id)
	if err != nil || status != execution.Failed {
		t.Fatalf("execution status = %q, error = %v", status, err)
	}

	before, err := app.GetExecutionStatus(ctx, created.ID, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	storage, err = jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	restarted := harness.New(storage)
	t.Cleanup(func() { _ = restarted.Shutdown(context.Background()) })

	app = New(config.Config{}, restarted, storage, nil, token.NewCharacterEstimator())

	loaded, err := app.GetExecutionStatus(ctx, created.ID, id)
	if err != nil || loaded != before {
		t.Fatalf("restored execution = %+v, error = %v; want %+v", loaded, err, before)
	}

	stored, err := app.WaitExecution(ctx, created.ID, id)
	if err != nil || stored != status {
		t.Fatalf("restored status = %q, error = %v; want %q", stored, err, status)
	}

	if _, err := app.GetExecutionStatus(ctx, created.ID, "missing"); !errors.Is(err, session.ErrExecutionNotFound) {
		t.Fatalf("unknown execution = %v", err)
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
			_, err := app.GetSession(ctx, "")

			return err
		},
		func() error {
			_, err := app.Usage(ctx, "")

			return err
		},
		func() error { return app.DeleteSession(ctx, "") },
		func() error {
			_, err := app.Execute(ctx, "", kit.NewTextContent("hello"))

			return err
		},
		func() error { return app.Cancel(ctx, "", "execution") },
		func() error {
			_, err := app.GetExecutionStatus(ctx, "", "execution")

			return err
		},
		func() error {
			_, err := app.WaitExecution(ctx, "", "execution")

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
		if err := operation(); !errors.Is(err, harness.ErrSessionRequired) {
			t.Fatalf("operation error = %v", err)
		}
	}

	if records, err := app.ListSessions(ctx); err != nil || len(records) != 0 {
		t.Fatalf("operations created sessions: %+v, %v", records, err)
	}
}

func TestRestartRestoresQueueUntilResume(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, execution.Func(
		func(ctx context.Context, _ execution.ExecuteParams) error {
			close(started)
			<-ctx.Done()

			return ctx.Err()
		},
	)))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	created, err := app.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	first, err := app.Execute(ctx, created.ID, kit.NewTextContent("first"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	submitted := []string{first}
	for _, input := range []string{"second", "third"} {
		id, err := app.Execute(ctx, created.ID, kit.NewTextContent(input))
		if err != nil {
			t.Fatal(err)
		}

		submitted = append(submitted, id)
	}

	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	storage, err = jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	ran := make(chan string, 4)
	restarted := harness.New(storage, harness.WithExecutor(agent.ExecutorName, execution.Func(
		func(_ context.Context, params execution.ExecuteParams) error {
			var input kit.Message
			if err := json.Unmarshal(params.Input, &input); err != nil {
				return err
			}

			ran <- input.TextContent().Text

			return nil
		},
	)))
	t.Cleanup(func() { _ = restarted.Shutdown(context.Background()) })

	app = New(config.Config{}, restarted, storage, nil, token.NewCharacterEstimator())

	for i, id := range submitted {
		want := execution.Queued
		if i == 0 {
			want = execution.Interrupted
		}

		if status, err := app.GetExecutionStatus(ctx, created.ID, id); err != nil || status != want {
			t.Fatalf("execution %s after restart = %q, %v", id, status, err)
		}
	}

	fourth, err := app.Execute(ctx, created.ID, kit.NewTextContent("fourth"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := app.GetExecutionStatus(ctx, created.ID, fourth); err != nil || status != execution.Queued {
		t.Fatalf("new work bypassed restored queue = %q, %v", status, err)
	}

	select {
	case input := <-ran:
		t.Fatalf("work ran before resume: %s", input)
	default:
	}

	resumed, err := app.Resume(ctx, created.ID)
	if err != nil || resumed != first {
		t.Fatalf("resumed execution = %s, %v; want %s", resumed, err, first)
	}

	if status, err := app.WaitExecution(ctx, created.ID, fourth); err != nil || status != execution.Completed {
		t.Fatalf("execution after restart = %q, %v", status, err)
	}

	for _, want := range []string{"first", "second", "third", "fourth"} {
		select {
		case input := <-ran:
			if input != want {
				t.Fatalf("execution order = %s, want %s", input, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
