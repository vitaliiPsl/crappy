package session

import (
	"context"
	"errors"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
)

type executeToolSet struct{}

func (executeToolSet) Add(...kit.Tool) {}

func (executeToolSet) Get(string) (kit.Tool, bool) {
	return nil, false
}

func (executeToolSet) List() []kit.Tool {
	return nil
}

func TestExecuteReturnsResponseAndMaintainsContext(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t,
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("first response")),
			FinishReason: kit.FinishReasonStop,
		}},
		kittest.ModelResult{Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("second response")),
			FinishReason: kit.FinishReasonStop,
		}},
	)
	store := entry.NewMemoryStore()
	session := New(State{ID: "session-1"}, store, agent.New(model, executeToolSet{}))

	first, err := session.Execute(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("first input")),
	)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	if first.Output == nil || first.Output.Text != "first response" {
		t.Fatalf("first Execute() output = %#v", first.Output)
	}

	second, err := session.Execute(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("second input")),
	)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	if second.Output == nil || second.Output.Text != "second response" {
		t.Fatalf("second Execute() output = %#v", second.Output)
	}

	request := model.CallAt(1)
	if got, want := len(request.Messages), 3; got != want {
		t.Fatalf("second model request has %d messages, want %d", got, want)
	}

	entries, err := session.Entries(context.Background(), 0)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if got, want := len(entries), 4; got != want {
		t.Fatalf("Entries() returned %d entries, want %d", got, want)
	}

	if entries[0].ExecutionID == "" || entries[0].ExecutionID != entries[1].ExecutionID {
		t.Fatalf("first execution IDs = %q and %q", entries[0].ExecutionID, entries[1].ExecutionID)
	}

	if entries[2].ExecutionID == "" || entries[2].ExecutionID != entries[3].ExecutionID {
		t.Fatalf("second execution IDs = %q and %q", entries[2].ExecutionID, entries[3].ExecutionID)
	}

	if entries[0].ExecutionID == entries[2].ExecutionID {
		t.Fatal("separate executions have the same execution ID")
	}
}

func TestExecuteRejectsConcurrentExecution(t *testing.T) {
	t.Parallel()

	model := &blockingModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	session := New(
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(model, executeToolSet{}),
	)

	finished := make(chan error, 1)
	go func() {
		_, err := session.Execute(
			context.Background(),
			kit.NewUserMessage(kit.NewTextContent("first input")),
		)
		finished <- err
	}()

	<-model.started

	_, err := session.Execute(
		context.Background(),
		kit.NewUserMessage(kit.NewTextContent("second input")),
	)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Execute() error = %v, want ErrBusy", err)
	}

	close(model.release)

	if err := <-finished; err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
}

type blockingModel struct {
	started chan struct{}
	release chan struct{}
}

func (m *blockingModel) Config() kit.ModelConfig {
	return kit.ModelConfig{ID: "blocking"}
}

func (m *blockingModel) Generate(
	ctx context.Context,
	_ kit.ModelRequest,
) (kit.ModelResponse, error) {
	return m.response(ctx)
}

func (m *blockingModel) Stream(
	ctx context.Context,
	_ kit.ModelRequest,
) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.NewStream(func(kit.Emitter[kit.ModelEvent]) (kit.ModelResponse, error) {
		return m.response(ctx)
	})
}

func (m *blockingModel) response(ctx context.Context) (kit.ModelResponse, error) {
	close(m.started)

	select {
	case <-m.release:
		return kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("done")),
			FinishReason: kit.FinishReasonStop,
		}, nil
	case <-ctx.Done():
		return kit.ModelResponse{}, ctx.Err()
	}
}
