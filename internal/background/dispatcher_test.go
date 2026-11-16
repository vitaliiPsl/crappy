package background

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestDispatcherSteersActiveExecutionOrStartsNewExecution(t *testing.T) {
	t.Parallel()

	for _, active := range []bool{true, false} {
		name := "idle"
		if active {
			name = "running"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			storage, err := jsonl.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			started := make(chan struct{})
			release := make(chan struct{})
			delivered := make(chan string, 1)
			h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
				accept := func(payload json.RawMessage) error {
					var message kit.Message
					if err := json.Unmarshal(payload, &message); err != nil {
						return err
					}

					text := kit.ContentsText(message.Content)
					if message.Role != kit.RoleUser || !strings.Contains(text, "job-id") || !strings.Contains(text, "job_get") {
						t.Errorf("completion notice = %+v", message)
					}

					delivered <- params.ExecutionID

					return params.Events.Commit(ctx, params.ExecutionID, agent.MessageEntryType, message)
				}
				if !active {
					return accept(params.Input)
				}

				close(started)

				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}

				return params.Steer(ctx, accept)
			})))
			t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

			opened, err := h.CreateSession(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}

			initialID := ""
			if active {
				current, err := opened.Execute(ctx, execution.NewRequest(agent.ExecutorName, kit.NewUserMessage(kit.NewTextContent("initial"))))
				if err != nil {
					t.Fatal(err)
				}

				initialID = current.ID()

				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}

			dispatcher := NewExecutionDispatcher(h)
			if err := dispatcher.Dispatch(ctx, Record{ID: "job-id", SessionID: opened.ID(), Tool: "bash", State: Completed}); err != nil {
				t.Fatal(err)
			}

			close(release)

			select {
			case id := <-delivered:
				if active && id != initialID {
					t.Fatalf("completion started another execution: %s, want %s", id, initialID)
				}

				current, err := opened.Execution(ctx, id)
				if err != nil {
					t.Fatal(err)
				}

				if status, err := current.Wait(ctx); err != nil || status != execution.Completed {
					t.Fatalf("completion execution = %s, %v", status, err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
