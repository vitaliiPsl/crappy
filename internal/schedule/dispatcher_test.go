package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestDispatcherSkipsOccurrenceWhilePreviousRunIsUnfinished(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	requests := make(chan string, 2)
	release := make(chan struct{})
	h := harness.New(memoryStore{sessions: state.NewMemoryStore(), entries: entry.NewMemoryStore()},
		harness.WithExecutor(agent.ExecutorName, execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
			var message kit.Message
			if err := json.Unmarshal(params.Input, &message); err != nil {
				return err
			}

			requests <- kit.ContentsText(message.Content)

			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})),
	)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	opened, err := h.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	dispatcher := NewExecutionDispatcher(h)
	record := Record{ID: "schedule-id", SessionID: opened.ID(), Instruction: "check the inbox", At: time.Now(), Cron: "0 0 * * *"}

	first, err := dispatcher.Dispatch(ctx, record)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case text := <-requests:
		for _, want := range []string{"schedule-id", "schedule_cancel", "check the inbox"} {
			if !strings.Contains(text, want) {
				t.Fatalf("request %q does not mention %q", text, want)
			}
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	record.ExecutionID = first
	if skipped, err := dispatcher.Dispatch(ctx, record); err != nil || skipped != first {
		t.Fatalf("dispatch during unfinished run = %s, %v; want %s", skipped, err, first)
	}

	close(release)
	waitCompleted(ctx, t, h, opened.ID(), first)

	second, err := dispatcher.Dispatch(ctx, record)
	if err != nil || second == first {
		t.Fatalf("dispatch after finished run = %s, %v", second, err)
	}

	waitCompleted(ctx, t, h, opened.ID(), second)

	entries, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	executions := make(map[string]struct{})
	for _, value := range entries {
		if value.Type == execution.EntryType {
			executions[value.ExecutionID] = struct{}{}
		}
	}

	if len(executions) != 2 {
		t.Fatalf("executions = %v, want %s and %s", executions, first, second)
	}
}

func TestDispatcherReportsMissingSession(t *testing.T) {
	t.Parallel()

	h := harness.New(memoryStore{sessions: state.NewMemoryStore(), entries: entry.NewMemoryStore()})
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	_, err := NewExecutionDispatcher(h).Dispatch(t.Context(), Record{ID: "schedule-id", SessionID: "missing", Instruction: "work"})
	if !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("dispatch to missing session = %v", err)
	}
}

func waitCompleted(ctx context.Context, t *testing.T, h *harness.Harness, sessionID, executionID string) {
	t.Helper()

	opened, err := h.Session(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	current, err := opened.Execution(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}

	if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
		t.Fatalf("execution %s = %s, %v", executionID, status, err)
	}
}

type memoryStore struct {
	sessions *state.MemoryStore
	entries  *entry.MemoryStore
}

func (s memoryStore) SessionStore() state.Store { return s.sessions }

func (s memoryStore) EntryStore() entry.Store { return s.entries }
