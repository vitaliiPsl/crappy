package workdir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestResolveFallsBackWithoutChangingSessionState(t *testing.T) {
	t.Chdir(t.TempDir())

	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	sessionState := newSessionState(t, state.NewMemoryStore(), "session")

	resolved, err := Resolve(ctx, sessionState)
	if err != nil {
		t.Fatal(err)
	}

	if resolved != directory {
		t.Fatalf("workdir = %q, want %q", resolved, directory)
	}

	var saved string
	if found, err := sessionState.Get(ctx, StateKey, &saved); err != nil || found {
		t.Fatalf("resolution changed session workdir: found = %v, error = %v", found, err)
	}
}

func TestResolveRejectsInvalidWorkdir(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "empty", value: ""},
		{name: "relative", value: "relative"},
		{name: "missing", value: filepath.Join(t.TempDir(), "missing")},
		{name: "file", value: file},
		{name: "wrong type", value: 42},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()

			sessionState := newSessionState(t, state.NewMemoryStore(), "session")
			if err := sessionState.Set(ctx, StateKey, test.value); err != nil {
				t.Fatal(err)
			}

			if _, err := Resolve(ctx, sessionState); err == nil {
				t.Fatal("invalid workdir was accepted")
			}
		})
	}
}

func TestResolvePropagatesStateErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := state.NewMemoryStore()
	sessionState := newSessionState(t, store, "session")

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := Resolve(canceled, sessionState); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled resolution = %v", err)
	}

	if err := store.Delete(ctx, "session"); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve(ctx, sessionState); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("deleted session resolution = %v", err)
	}
}

func newSessionState(t *testing.T, store state.Store, id string) state.State {
	t.Helper()

	if err := store.Save(context.Background(), state.Record{ID: id}); err != nil {
		t.Fatal(err)
	}

	return state.New(store, id)
}
