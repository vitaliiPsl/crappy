package session

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestSteeringAcceptancePreservesInputOnFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	callbacks := make(chan func(context.Context, func(json.RawMessage) error) error, 1)
	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		callbacks <- params.Steer

		<-ctx.Done()

		return ctx.Err()
	})
	opened := newExecutorSession(ctx, t, "session", entry.NewMemoryStore(), "work", executor)

	first, err := opened.Execute(ctx, execution.NewRequest("work", "first"))
	if err != nil {
		t.Fatal(err)
	}

	var receive func(context.Context, func(json.RawMessage) error) error
	select {
	case receive = <-callbacks:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	for _, text := range []string{"one", "two"} {
		if err := opened.Steer(ctx, execution.NewRequest("work", text)); err != nil {
			t.Fatal(err)
		}
	}

	failure := errors.New("cannot record input")

	var received []string

	accept := func(input json.RawMessage) error {
		var text string
		if err := json.Unmarshal(input, &text); err != nil {
			return err
		}

		received = append(received, text)

		return nil
	}
	if err := receive(ctx, func(input json.RawMessage) error {
		if string(input) == `"two"` {
			return failure
		}

		return accept(input)
	}); !errors.Is(err, failure) {
		t.Fatalf("acceptance error = %v", err)
	}

	if err := receive(ctx, accept); err != nil {
		t.Fatal(err)
	}

	if err := receive(ctx, accept); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(received, []string{"one", "two"}) {
		t.Fatalf("steering input = %v", received)
	}

	if status, err := first.Status(); err != nil || status != execution.Running {
		t.Fatalf("steering replaced active execution: %s, %v", status, err)
	}

	if len(opened.queue) != 0 {
		t.Fatal("active steering created executions")
	}
}

func TestSteeringReceivesSnapshotWithoutHoldingSessionLock(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	release := make(chan struct{})

	var received []string

	var opened *Session

	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		close(started)

		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}

		accept := func(input json.RawMessage) error {
			var text string
			if err := json.Unmarshal(input, &text); err != nil {
				return err
			}

			received = append(received, text)

			if text == "one" {
				return opened.Steer(ctx, execution.NewRequest("work", "later"))
			}

			return nil
		}
		if err := params.Steer(ctx, accept); err != nil {
			return err
		}

		if !slices.Equal(received, []string{"one", "two"}) {
			return errors.New("received input submitted after the boundary")
		}

		return params.Steer(ctx, accept)
	})
	opened = newExecutorSession(ctx, t, "session", entry.NewMemoryStore(), "work", executor)

	active, err := opened.Execute(ctx, execution.NewRequest("work", "initial"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	for _, text := range []string{"one", "two"} {
		if err := opened.Steer(ctx, execution.NewRequest("work", text)); err != nil {
			t.Fatal(err)
		}
	}

	close(release)

	if status, err := active.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("steering execution = %s, %v", status, err)
	}

	if !slices.Equal(received, []string{"one", "two", "later"}) {
		t.Fatalf("steering input = %v", received)
	}
}

func TestSteeringFallsBackToQueueWhenExecutionEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan execution.ExecuteParams, 2)
	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		started <- params

		if string(params.Input) == `"first"` {
			<-ctx.Done()
		}

		return ctx.Err()
	})
	opened := newExecutorSession(ctx, t, "session", entry.NewMemoryStore(), "work", executor)

	first, err := opened.Execute(ctx, execution.NewRequest("work", "first"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := opened.Steer(ctx, execution.NewRequest("work", "next")); err != nil {
		t.Fatal(err)
	}

	if err := opened.Cancel(ctx, first.ID()); err != nil {
		t.Fatal(err)
	}

	var next execution.ExecuteParams
	select {
	case next = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if string(next.Input) != `"next"` || next.ExecutionID == first.ID() {
		t.Fatalf("fallback input = %s, execution = %s", next.Input, next.ExecutionID)
	}

	accepted, err := opened.Execution(ctx, next.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}

	if status, err := accepted.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("queued fallback = %s, %v", status, err)
	}
}

func TestSteeringQueuesWhenIdleOrExecutorDiffers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan execution.ExecuteParams, 3)
	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		started <- params

		if string(params.Input) == `"hold"` {
			<-ctx.Done()
		}

		return ctx.Err()
	})
	registry := execution.NewRegistry()
	registry.Register("first", executor)
	registry.Register("other", executor)
	opened := newSession(ctx, t, "session", entry.NewMemoryStore(), registry)

	if err := opened.Steer(ctx, execution.NewRequest("first", "hold")); err != nil {
		t.Fatal(err)
	}

	var active execution.ExecuteParams
	select {
	case active = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := opened.Steer(ctx, execution.NewRequest("other", "different")); err != nil {
		t.Fatal(err)
	}

	var received int
	if err := active.Steer(ctx, func(json.RawMessage) error {
		received++

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if received != 0 {
		t.Fatal("steering crossed executor types")
	}

	if err := opened.Cancel(ctx, active.ExecutionID); err != nil {
		t.Fatal(err)
	}

	select {
	case next := <-started:
		if string(next.Input) != `"different"` {
			t.Fatalf("queued input = %s", next.Input)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestSteeringShutdownQueuesUnconsumedInputForRecovery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	started := make(chan struct{})
	executor := execution.Func(func(ctx context.Context, _ execution.ExecuteParams) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	})
	store := entry.NewMemoryStore()
	opened := newExecutorSession(ctx, t, "session", store, "work", executor)

	first, err := opened.Execute(ctx, execution.NewRequest("work", "first"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := opened.Steer(ctx, execution.NewRequest("work", "pending")); err != nil {
		t.Fatal(err)
	}

	opened.Close()

	restored := newExecutorSession(ctx, t, "session", store, "work", executor)
	if restored.current == nil || restored.current.ID() != first.ID() || len(restored.queue) != 1 || string(restored.queue[0].record.Input) != `"pending"` {
		t.Fatalf("unconsumed input was not restored as queued work: %+v", restored.queue)
	}
}
