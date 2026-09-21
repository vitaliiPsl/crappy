package runtime

import (
	"context"
	"errors"
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

func TestRuntimeDeletesClosedSession(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{}
	runtime := NewRuntime(context.Background(), store, nil, nil, nil)

	if err := runtime.DeleteSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if store.deleted != "session-1" {
		t.Fatalf("deleted session = %q, want session-1", store.deleted)
	}
}

func TestRuntimeRejectsDeletingOpenSession(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{}
	runtime := NewRuntime(
		context.Background(),
		store,
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	if _, err := runtime.Session(context.Background(), "session-1"); err != nil {
		t.Fatalf("Session() error = %v", err)
	}

	err := runtime.DeleteSession(context.Background(), "session-1")
	if !errors.Is(err, ErrSessionOpen) {
		t.Fatalf("DeleteSession() error = %v, want ErrSessionOpen", err)
	}

	if store.deleted != "" {
		t.Fatalf("deleted session = %q, want none", store.deleted)
	}
}

type staticSessionStore struct {
	states []session.State
}

func (s staticSessionStore) List(context.Context) ([]session.State, error) {
	return s.states, nil
}

func (staticSessionStore) Delete(context.Context, string) error {
	return nil
}

type recordingSessionStore struct {
	deleted string
}

func (*recordingSessionStore) List(context.Context) ([]session.State, error) {
	return nil, nil
}

func (s *recordingSessionStore) Delete(_ context.Context, sessionID string) error {
	s.deleted = sessionID

	return nil
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
