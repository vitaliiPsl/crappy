package background

import (
	"context"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestWorkerDispatchesCompletionsAndCancelsDispatchOnShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	manager := newTestManager(t)
	dispatched := make(chan Record, 1)
	cancelled := make(chan struct{})
	worker := NewWorker(manager, dispatchFunc(func(ctx context.Context, record Record) error {
		dispatched <- record

		<-ctx.Done()
		close(cancelled)

		return ctx.Err()
	}))
	worker.Start()
	t.Cleanup(func() {
		if err := worker.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})

	record, err := manager.Start(ctx, "session", "bash", time.Minute, func(context.Context) (kit.ToolOutput, error) {
		return kit.NewToolOutput(kit.NewTextContent("done")), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-dispatched:
		if got.ID != record.ID || got.SessionID != "session" || got.State != Completed {
			t.Fatalf("dispatched = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := worker.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-cancelled:
	default:
		t.Fatal("shutdown returned before dispatch was cancelled")
	}
}

type dispatchFunc func(context.Context, Record) error

func (f dispatchFunc) Dispatch(ctx context.Context, record Record) error {
	return f(ctx, record)
}
