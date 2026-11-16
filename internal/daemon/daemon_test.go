package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/api"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/workdir"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type serverFixture struct {
	client *api.Client
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func newSocketRoot(t *testing.T) string {
	t.Helper()
	// Darwin's Unix socket limit can be shorter than a t.TempDir path.
	root, err := os.MkdirTemp("/tmp", "crappy-test-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	return root
}

func newServerFixture(t *testing.T, root string, executor execution.Executor) *serverFixture {
	t.Helper()

	instance, err := Acquire(filepath.Join(root, "runtime"))
	if errors.Is(err, ErrRunning) {
		t.Skip("another process owns the runtime")
	}

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = instance.Close() })

	listener, err := instance.Listen()
	if err != nil {
		t.Fatal(err)
	}

	storage, err := jsonl.Open(t.Context(), filepath.Join(root, "sessions"))
	if err != nil {
		_ = listener.Close()

		t.Fatal(err)
	}

	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, executor))
	app := service.NewLocal(config.Config{ProviderID: "test", ModelID: "test-model"}, h, storage, nil, token.NewCharacterEstimator(), nil)
	ctx, cancel := context.WithCancel(context.Background())

	fixture := &serverFixture{
		client: api.NewClient(filepath.Join(root, "runtime", "socket"), root),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		defer close(fixture.done)

		fixture.err = New(app, h.Shutdown).Run(ctx, listener)
	}()

	t.Cleanup(func() {
		cancel()
		<-fixture.done
		fixture.client.Close()

		if fixture.err != nil {
			t.Errorf("server: %v", fixture.err)
		}
	})

	return fixture
}

func commitResult(ctx context.Context, params execution.ExecuteParams, text string) error {
	message := kit.NewModelMessage(kit.NewTextContent(text))
	message.Usage = kit.Usage{InputTokens: 7, OutputTokens: 4}

	return params.Events.Commit(ctx, params.ExecutionID, agent.MessageEntryType, message)
}

func TestDaemonSteersRunningExecutionAndQueuesWhenIdle(t *testing.T) {
	type delivery struct {
		executionID string
		message     kit.Message
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	started := make(chan string, 2)
	received := make(chan delivery, 2)
	release := make(chan struct{})
	fixture := newServerFixture(t, newSocketRoot(t), execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		var input kit.Message
		if err := json.Unmarshal(params.Input, &input); err != nil {
			return err
		}

		started <- params.ExecutionID

		record := func(message kit.Message) error {
			if err := params.Events.Commit(ctx, params.ExecutionID, agent.MessageEntryType, message); err != nil {
				return err
			}

			received <- delivery{executionID: params.ExecutionID, message: message}

			return nil
		}
		if input.TextContent().Text != "work" {
			return record(input)
		}

		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}

		return params.Steer(ctx, func(payload json.RawMessage) error {
			var message kit.Message
			if err := json.Unmarshal(payload, &message); err != nil {
				return err
			}

			return record(message)
		})
	}))

	if err := fixture.client.Steer(ctx, "missing", kit.NewTextContent("focus")); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("unknown session = %v", err)
	}

	opened, err := fixture.client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := fixture.client.Execute(ctx, opened.ID, kit.NewTextContent("work"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case id := <-started:
		if id != first {
			t.Fatalf("started execution = %s, want %s", id, first)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := fixture.client.Steer(ctx, opened.ID, kit.NewTextContent("focus"), kit.NewTextContent("also check tests")); err != nil {
		t.Fatal(err)
	}

	close(release)

	if status, err := fixture.client.WaitExecution(ctx, opened.ID, first); err != nil || status != execution.Completed {
		t.Fatalf("steered execution = %s, %v", status, err)
	}

	if err := fixture.client.Steer(ctx, opened.ID, kit.NewTextContent("idle")); err != nil {
		t.Fatal(err)
	}

	var nextID string
	select {
	case id := <-started:
		nextID = id

		if id == first {
			t.Fatal("idle steering reused the completed execution")
		}

		if status, err := fixture.client.WaitExecution(ctx, opened.ID, id); err != nil || status != execution.Completed {
			t.Fatalf("idle steering = %s, %v", status, err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	for _, want := range []struct {
		executionID string
		text        string
	}{
		{executionID: first, text: "focus\nalso check tests"},
		{executionID: nextID, text: "idle"},
	} {
		select {
		case got := <-received:
			if got.executionID != want.executionID || got.message.Role != kit.RoleUser || kit.ContentsText(got.message.Content) != want.text {
				t.Fatalf("delivered input = %+v, want %+v", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestDaemonKeepsExecutionAliveAfterClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	root := newSocketRoot(t)
	started, release := make(chan struct{}), make(chan struct{})
	fixture := newServerFixture(t, root, execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		close(started)

		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}

		if err := params.Events.Emit(ctx, params.ExecutionID, "progress", "finishing"); err != nil {
			return err
		}

		return commitResult(ctx, params, "done")
	}))
	requestContext, disconnect := context.WithCancel(ctx)

	session, err := fixture.client.CreateSession(requestContext, "")
	if err != nil {
		t.Fatal(err)
	}

	id, err := fixture.client.Execute(requestContext, session.ID, kit.NewTextContent("work"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	subscription, err := fixture.client.Subscribe(requestContext, session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	var cursor uint64
	select {
	case value := <-subscription.Events():
		cursor = value.EntryID
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	disconnect()
	subscription.Close()
	fixture.client.Close()

	client := api.NewClient(filepath.Join(root, "runtime", "socket"), filepath.Join(root, "other"))
	defer client.Close()

	if status, err := client.GetExecutionStatus(ctx, session.ID, id); err != nil || status != execution.Running {
		t.Fatalf("after disconnect: %s, %v", status, err)
	}

	reconnected, err := client.Subscribe(ctx, session.ID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()

	close(release)

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("outcome: %s, %v", status, err)
	}

	sawProgress, sawMessage := false, false
	for {
		select {
		case value, ok := <-reconnected.Events():
			if !ok {
				t.Fatalf("stream ended: %v", reconnected.Err())
			}

			if value.EntryID != 0 && value.EntryID <= cursor {
				t.Fatalf("replayed old entry: %d <= %d", value.EntryID, cursor)
			}

			if value.Type == "progress" {
				sawProgress = true
			}

			if value.Type == agent.MessageEntryType {
				sawMessage = true
			}

			if value.Type == execution.EntryType {
				var record execution.Record

				_ = json.Unmarshal(value.Payload, &record)
				if record.Status == execution.Completed {
					if !sawProgress || !sawMessage {
						t.Fatalf("progress=%t message=%t", sawProgress, sawMessage)
					}

					return
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestDaemonSessionOperationsAndErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	root := newSocketRoot(t)
	fixture := newServerFixture(t, root, execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		return commitResult(ctx, params, "done")
	}))

	first := api.NewClient(filepath.Join(root, "runtime", "socket"), filepath.Join(root, "first"))
	defer first.Close()

	second := api.NewClient(filepath.Join(root, "runtime", "socket"), filepath.Join(root, "second"))
	defer second.Close()

	for index, client := range []*api.Client{first, second} {
		wantDirectory := filepath.Join(root, []string{"first", "second"}[index])

		session, err := client.CreateSession(ctx, "")
		if err != nil {
			t.Fatal(err)
		}

		var directory string
		if err := json.Unmarshal(session.Values[workdir.StateKey], &directory); err != nil || directory != wantDirectory {
			t.Fatalf("workdir=%q, error=%v", directory, err)
		}

		if _, err := client.GetSession(ctx, session.ID); err != nil {
			t.Fatal(err)
		}

		id, err := client.Execute(ctx, session.ID, kit.NewTextContent("work"))
		if err != nil {
			t.Fatal(err)
		}

		if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
			t.Fatalf("wait=%s, %v", status, err)
		}

		usage, err := client.Usage(ctx, session.ID)
		if err != nil || usage.InputTokens != 7 || usage.OutputTokens != 4 {
			t.Fatalf("usage=%+v, %v", usage, err)
		}

		if err := client.DeleteSession(ctx, session.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := client.GetSession(ctx, session.ID); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("missing session: %v", err)
		}
	}

	if sessions, err := fixture.client.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("sessions=%+v, %v", sessions, err)
	}

	if model, err := fixture.client.GetModel(ctx); err != nil || model.ModelID != "test-model" {
		t.Fatalf("model=%+v, %v", model, err)
	}

	if _, err := fixture.client.Context(ctx, ""); !errors.Is(err, harness.ErrSessionRequired) {
		t.Fatalf("empty session: %v", err)
	}

	if _, err := fixture.client.Subscribe(ctx, "missing", 0); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing subscription: %v", err)
	}
}

func TestDaemonReportsLostEventConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	fixture := newServerFixture(t, newSocketRoot(t), execution.Func(func(context.Context, execution.ExecuteParams) error { return nil }))

	session, err := fixture.client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := fixture.client.Subscribe(ctx, session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	fixture.cancel()

	select {
	case <-subscription.Done():
		if subscription.Err() == nil {
			t.Fatal("lost connection reported as clean close")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
