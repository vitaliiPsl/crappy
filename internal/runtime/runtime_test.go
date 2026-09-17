package runtime

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestRuntimeOwnsSession(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	first, err := runtime.Session(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("first Session() error = %v", err)
	}

	second, err := runtime.Session(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("second Session() error = %v", err)
	}

	if first != second {
		t.Fatal("Session() returned different instances for the same ID")
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
