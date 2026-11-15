package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWorkerShutdownCancelsAndWaitsForDispatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	manager, err := NewManager(ctx, NewJSONStore(filepath.Join(t.TempDir(), "schedules.json")))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Create(ctx, "session", "work", time.Now()); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})

	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()

	worker := NewWorker(manager, dispatchFunc(func(ctx context.Context, _ Record) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release

		return ctx.Err()
	}), time.Hour)
	worker.Start()
	t.Cleanup(func() {
		unblock()

		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		if err := worker.Shutdown(cleanup); err != nil {
			t.Error(err)
		}
	})

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	deadline, stopWaiting := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopWaiting()

	if err := worker.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown returned before dispatch settled: %v", err)
	}

	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("dispatch did not receive cancellation")
	}

	unblock()

	if err := worker.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
