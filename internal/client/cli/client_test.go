package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

func TestClientRunsSession(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Events: []kit.ModelEvent{
			kit.NewModelContentDeltaEvent(kit.NewTextContent("hello")),
		},
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})
	runtime := runtime.New(
		context.Background(),
		stubSessionStore{},
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil,
	)

	var output bytes.Buffer

	err := New(
		runtime,
		"session-1",
		strings.NewReader("hi\n"),
		&output,
	).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := output.String(); !strings.Contains(got, "hello") {
		t.Fatalf("output = %q, want agent response", got)
	}
}

type stubSessionStore struct{}

func (stubSessionStore) List(context.Context) ([]session.State, error) {
	return nil, nil
}

func (stubSessionStore) Get(_ context.Context, sessionID string) (session.State, error) {
	return session.State{ID: sessionID}, nil
}

func (stubSessionStore) Save(context.Context, session.State) error {
	return nil
}

func (stubSessionStore) Delete(context.Context, string) error {
	return nil
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
