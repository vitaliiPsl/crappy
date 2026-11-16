package background

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestManagerJobOutlivesCallerDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	manager := newTestManager(t)

	caller, stopCaller := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopCaller()

	started := make(chan context.Context, 1)
	release := make(chan struct{})

	record, err := manager.Start(caller, "session", "bash", time.Minute, func(ctx context.Context) (kit.ToolOutput, error) {
		started <- ctx

		select {
		case <-release:
			return kit.NewToolOutput(kit.NewTextContent("done")), nil
		case <-ctx.Done():
			return kit.ToolOutput{}, ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	var running context.Context
	select {
	case running = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	<-caller.Done()

	if running.Err() != nil {
		t.Fatalf("caller deadline stopped background work: %v", running.Err())
	}

	deadline, ok := running.Deadline()
	if !ok || time.Until(deadline) < 30*time.Second {
		t.Fatalf("job deadline = %s", deadline)
	}

	close(release)

	if finished := receiveCompletion(ctx, t, manager); finished.ID != record.ID || finished.State != Completed {
		t.Fatalf("completion = %+v", finished)
	}

	result, err := manager.Get(ctx, "session", record.ID)
	if err != nil || kit.ContentsText(result.Output.Content) != "done" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestManagerScopesJobsAndCancelsRunningWork(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)

	record, err := manager.Start(t.Context(), "first", "bash", time.Minute, func(ctx context.Context) (kit.ToolOutput, error) {
		<-ctx.Done()

		return kit.ToolOutput{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Get(t.Context(), "other", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-session get = %v", err)
	}

	if _, err := manager.Cancel(t.Context(), "other", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-session cancel = %v", err)
	}

	if records, err := manager.List(t.Context(), "other"); err != nil || len(records) != 0 {
		t.Fatalf("cross-session list = %+v, %v", records, err)
	}

	if _, err := manager.Cancel(t.Context(), "first", record.ID); err != nil {
		t.Fatal(err)
	}

	if finished := receiveCompletion(t.Context(), t, manager); finished.State != Cancelled {
		t.Fatalf("cancelled job = %+v", finished)
	}

	if repeated, err := manager.Cancel(t.Context(), "first", record.ID); err != nil || repeated.State != Cancelled {
		t.Fatalf("repeated cancellation = %+v, %v", repeated, err)
	}
}

func TestManagerRecordsTimeoutErrorsAndPanics(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		timeout time.Duration
		execute func(context.Context) (kit.ToolOutput, error)
		error   string
	}{
		{
			name:    "timeout",
			timeout: 20 * time.Millisecond,
			execute: func(ctx context.Context) (kit.ToolOutput, error) {
				<-ctx.Done()

				return kit.ToolOutput{}, nil
			},
			error: context.DeadlineExceeded.Error(),
		},
		{
			name:    "tool error",
			timeout: time.Minute,
			execute: func(context.Context) (kit.ToolOutput, error) {
				return kit.ToolOutput{}, errors.New("command failed")
			},
			error: "command failed",
		},
		{
			name:    "panic",
			timeout: time.Minute,
			execute: func(context.Context) (kit.ToolOutput, error) {
				panic("broken tool")
			},
			error: "panicked: broken tool",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestManager(t)

			record, err := manager.Start(t.Context(), "session", "bash", test.timeout, test.execute)
			if err != nil {
				t.Fatal(err)
			}

			finished := receiveCompletion(t.Context(), t, manager)

			result, err := manager.Get(t.Context(), "session", record.ID)
			if err != nil || finished.State != Failed || !strings.Contains(result.Error, test.error) {
				t.Fatalf("failed job = %+v, %v", result, err)
			}
		})
	}
}

func TestManagerShutdownCancelsAndWaitsForTools(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	cancelled := make(chan struct{})
	release := make(chan struct{})

	_, err := manager.Start(t.Context(), "session", "bash", time.Minute, func(ctx context.Context) (kit.ToolOutput, error) {
		<-ctx.Done()
		close(cancelled)
		<-release

		return kit.ToolOutput{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	defer close(release)

	deadline, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if err := manager.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown returned before tool cleanup: %v", err)
	}

	select {
	case <-cancelled:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}

	_, err = manager.Start(t.Context(), "session", "bash", time.Second, func(context.Context) (kit.ToolOutput, error) {
		t.Error("job started after shutdown")

		return kit.ToolOutput{}, nil
	})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("start after shutdown = %v", err)
	}
}

func TestManagerEnforcesConfiguredTimeoutLimit(t *testing.T) {
	t.Parallel()

	manager := NewManager(2 * time.Second)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	for _, timeout := range []time.Duration{0, -time.Second, 2*time.Second + time.Nanosecond} {
		_, err := manager.Start(t.Context(), "session", "bash", timeout, func(context.Context) (kit.ToolOutput, error) {
			t.Error("started a job with an invalid timeout")

			return kit.ToolOutput{}, nil
		})
		if err == nil {
			t.Fatalf("accepted timeout %s", timeout)
		}
	}

	if records, err := manager.List(t.Context(), "session"); err != nil || len(records) != 0 {
		t.Fatalf("invalid timeouts created jobs: %+v, %v", records, err)
	}

	_, err := manager.Start(t.Context(), "session", "bash", 2*time.Second, func(context.Context) (kit.ToolOutput, error) {
		return kit.NewToolOutput(kit.NewTextContent("done")), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if finished := receiveCompletion(t.Context(), t, manager); finished.State != Completed {
		t.Fatalf("job at the timeout limit = %+v", finished)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager := NewManager(time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := manager.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})

	return manager
}

func receiveCompletion(ctx context.Context, t *testing.T, manager *Manager) Record {
	t.Helper()

	select {
	case record := <-manager.Completions():
		return record
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	return Record{}
}
