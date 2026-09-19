package runtime

import (
	"context"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session"
)

func TestRuntimeOwnsSession(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(
		context.Background(),
		staticSessionStore{},
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

func TestRuntimeListsStoredSessions(t *testing.T) {
	t.Parallel()

	want := []session.State{{ID: "session-1"}}
	runtime := NewRuntime(
		context.Background(),
		staticSessionStore{states: want},
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	got, err := runtime.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}

	if len(got) != 1 || got[0].ID != want[0].ID {
		t.Fatalf("Sessions() = %#v, want %#v", got, want)
	}
}

type staticSessionStore struct {
	states []session.State
}

func (s staticSessionStore) List(context.Context) ([]session.State, error) {
	return s.states, nil
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
