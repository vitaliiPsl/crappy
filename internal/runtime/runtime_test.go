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

	runtime := New(
		context.Background(),
		staticSessionStore{states: []session.State{{ID: "session-1"}}},
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	first, err := runtime.Open(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}

	second, err := runtime.Open(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}

	if first != second {
		t.Fatal("Open() returned different instances for the same ID")
	}
}

func TestRuntimeListsStoredSessions(t *testing.T) {
	t.Parallel()

	want := []session.State{{ID: "session-1"}}
	runtime := New(
		context.Background(),
		staticSessionStore{states: want},
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	got, err := runtime.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(got) != 1 || got[0].ID != want[0].ID {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
}

func TestRuntimeDeletesClosedSession(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{}
	runtime := New(context.Background(), store, nil, nil, nil)

	if err := runtime.Delete(context.Background(), "session-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if store.deleted != "session-1" {
		t.Fatalf("deleted session = %q, want session-1", store.deleted)
	}
}

func TestRuntimeDeletesOpenSession(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{saved: []session.State{{ID: "session-1"}}}
	runtime := New(
		context.Background(),
		store,
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{kittest.NewModel(t)}),
		nil,
	)

	opened, err := runtime.Open(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := runtime.Delete(context.Background(), "session-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if store.deleted != "session-1" {
		t.Fatalf("deleted session = %q, want session-1", store.deleted)
	}

	_, err = opened.Execute(kit.NewUserMessage(kit.NewTextContent("input")))
	if !errors.Is(err, session.ErrClosed) {
		t.Fatalf("Execute() on deleted session error = %v, want ErrClosed", err)
	}

	reopened, err := runtime.Open(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Open() after delete error = %v", err)
	}

	if reopened == opened {
		t.Fatal("Open() after delete returned the closed instance")
	}
}

func TestRuntimeCreateSavesSession(t *testing.T) {
	t.Parallel()

	store := &recordingSessionStore{}
	runtime := New(context.Background(), store, entry.NewMemoryStore(), nil, nil)

	created, err := runtime.Create(context.Background(), CreateOptions{Title: "Fix compaction"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if created.ID == "" || created.Title != "Fix compaction" || created.CreatedAt.IsZero() {
		t.Fatalf("Create() = %#v, want titled session with ID and timestamps", created)
	}

	if len(store.saved) != 1 || store.saved[0] != created {
		t.Fatalf("saved = %#v, want created session", store.saved)
	}
}

func TestRuntimeOpenRequiresCreatedSession(t *testing.T) {
	t.Parallel()

	runtime := New(context.Background(), &recordingSessionStore{}, entry.NewMemoryStore(), nil, nil)

	if _, err := runtime.Open(context.Background(), "missing"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Open() error = %v, want ErrNotFound", err)
	}
}

type staticSessionStore struct {
	states []session.State
}

func (s staticSessionStore) List(context.Context) ([]session.State, error) {
	return s.states, nil
}

func (s staticSessionStore) Get(_ context.Context, sessionID string) (session.State, error) {
	for _, state := range s.states {
		if state.ID == sessionID {
			return state, nil
		}
	}

	return session.State{}, session.ErrNotFound
}

func (staticSessionStore) Save(context.Context, session.State) error {
	return nil
}

func (staticSessionStore) Delete(context.Context, string) error {
	return nil
}

type recordingSessionStore struct {
	deleted string
	saved   []session.State
}

func (*recordingSessionStore) List(context.Context) ([]session.State, error) {
	return nil, nil
}

func (s *recordingSessionStore) Get(_ context.Context, sessionID string) (session.State, error) {
	for i := len(s.saved) - 1; i >= 0; i-- {
		if s.saved[i].ID == sessionID {
			return s.saved[i], nil
		}
	}

	return session.State{}, session.ErrNotFound
}

func (s *recordingSessionStore) Save(_ context.Context, state session.State) error {
	s.saved = append(s.saved, state)

	return nil
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
