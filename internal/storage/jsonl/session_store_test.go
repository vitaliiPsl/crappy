package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestSessionStoreListsMaterializedSessions(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)

	states, err := storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(states) != 0 {
		t.Fatalf("List() = %#v, want no empty sessions", states)
	}

	if _, err := storage.EntryStore().Append(
		context.Background(),
		"session-2",
		entry.Entry{Type: "test.entry"},
		entry.Entry{Type: "test.entry"},
	); err != nil {
		t.Fatalf("Append(session-2) error = %v", err)
	}

	if _, err := storage.EntryStore().Append(
		context.Background(),
		"session-1",
		entry.Entry{Type: "test.entry"},
	); err != nil {
		t.Fatalf("Append(session-1) error = %v", err)
	}

	storage = openStorage(t, root)

	states, err = storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatalf("List() after reopen error = %v", err)
	}

	if len(states) != 2 {
		t.Fatalf("List() = %#v, want both sessions", states)
	}
}

func TestSessionStoreSavesAndGetsState(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	createdAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	want := state.Record{ID: "session-1", CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Hour)}

	if err := openStorage(t, root).SessionStore().Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := openStorage(t, root).SessionStore().Get(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) || got.ID != want.ID {
		t.Fatalf("Get() = %#v, want %#v", got, want)
	}

	assertMode(t, filepath.Join(root, "session-1", stateFilename), 0o600)
}

func TestSessionStoreGetReturnsNotFound(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))

	if _, err := storage.SessionStore().Get(context.Background(), "missing"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestSessionStoreListsMostRecentlyUpdatedFirst(t *testing.T) {
	t.Parallel()

	store := openStorage(t, filepath.Join(t.TempDir(), "sessions")).SessionStore()
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	for id, updatedAt := range map[string]time.Time{
		"older":  base,
		"newest": base.Add(2 * time.Hour),
		"newer":  base.Add(time.Hour),
	} {
		if err := store.Save(context.Background(), state.Record{ID: id, CreatedAt: base, UpdatedAt: updatedAt}); err != nil {
			t.Fatalf("Save(%s) error = %v", id, err)
		}
	}

	states, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	got := make([]string, len(states))
	for i, value := range states {
		got[i] = value.ID
	}

	if want := []string{"newest", "newer", "older"}; !slices.Equal(got, want) {
		t.Fatalf("List() order = %v, want %v", got, want)
	}
}

func TestSessionStoreListsSessionWithoutEntries(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)

	if err := os.Mkdir(filepath.Join(root, "session-1"), 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	states, err := storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(states) != 1 || states[0].ID != "session-1" {
		t.Fatalf("List() = %#v, want session without entries", states)
	}
}

func TestSessionStoreDeletesSession(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)
	entries := storage.EntryStore()

	if _, err := entries.Append(context.Background(), "session-1", entry.Entry{
		Type: "test.entry",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if err := storage.SessionStore().Delete(context.Background(), "session-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	states, err := storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(states) != 0 {
		t.Fatalf("List() = %#v, want deleted session absent", states)
	}

	remaining, err := entries.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("entries List() error = %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("entries = %#v, want deleted transcript absent", remaining)
	}
}

func TestSessionStoreHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := storage.SessionStore().List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}

	if err := storage.SessionStore().Delete(ctx, "session-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() error = %v, want context.Canceled", err)
	}
}

func TestSessionStoreRejectsUnsafeSessionID(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))
	for _, sessionID := range []string{"", ".", "..", "nested/session", `nested\session`} {
		if err := storage.SessionStore().Delete(
			context.Background(),
			sessionID,
		); err == nil {
			t.Fatalf("Delete(%q) error = nil", sessionID)
		}
	}
}

func TestSessionStorePersistsCompleteRecord(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := openStorage(t, root)

	record := state.Record{ID: "first", CreatedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}
	if err := storage.SessionStore().Save(ctx, record); err != nil {
		t.Fatal(err)
	}

	accessor := state.New(storage.SessionStore(), record.ID)

	value := "unchanged"
	if found, err := accessor.Get(ctx, "missing", &value); err != nil || found || value != "unchanged" {
		t.Fatalf("missing value = %q, found = %v, error = %v", value, found, err)
	}

	want := map[string][]string{"paths": {"/project"}}
	if err := accessor.Set(ctx, "assistant.context", want); err != nil {
		t.Fatal(err)
	}

	want["paths"][0] = "/changed"

	if err := storage.SessionStore().Update(ctx, record.ID, func(record *state.Record) error {
		record.UpdatedAt = record.CreatedAt.Add(time.Hour)

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := openStorage(t, root).SessionStore().Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !loaded.CreatedAt.Equal(record.CreatedAt) || !loaded.UpdatedAt.Equal(record.CreatedAt.Add(time.Hour)) || len(loaded.Values) != 1 {
		t.Fatalf("reopened record = %+v", loaded)
	}

	reopened := state.New(openStorage(t, root).SessionStore(), record.ID)

	var got map[string][]string
	if found, err := reopened.Get(ctx, "assistant.context", &got); err != nil || !found || got["paths"][0] != "/project" {
		t.Fatalf("persisted value = %v, found = %v, error = %v", got, found, err)
	}

	assertMode(t, filepath.Join(root, record.ID, stateFilename), 0o600)

	if err := reopened.Delete(ctx, "assistant.context"); err != nil {
		t.Fatal(err)
	}

	if found, err := state.New(openStorage(t, root).SessionStore(), record.ID).Get(ctx, "assistant.context", &got); err != nil || found {
		t.Fatalf("deleted value: found = %v, error = %v", found, err)
	}

	if err := reopened.Delete(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStorePreservesCorruptFiles(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{"unfinished":`} {
		t.Run(data, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()

			storage := openStorage(t, root)
			if err := storage.SessionStore().Save(ctx, state.Record{ID: "session"}); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(root, "session", stateFilename)
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}

			accessor := state.New(storage.SessionStore(), "session")
			if err := accessor.Set(ctx, "key", "value"); err == nil {
				t.Fatal("Set overwrote corrupt state")
			}

			if err := accessor.Delete(ctx, "key"); err == nil {
				t.Fatal("Delete overwrote corrupt state")
			}

			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("state file = %q, error = %v", got, err)
			}
		})
	}
}

func TestSessionStoreUpdatesRecordsAtomically(t *testing.T) {
	ctx := context.Background()
	store := openStorage(t, t.TempDir()).SessionStore()

	original := state.Record{
		ID:        "session",
		UpdatedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
		Values:    map[string]json.RawMessage{"value": json.RawMessage(`"original"`)},
	}
	if err := store.Save(ctx, original); err != nil {
		t.Fatal(err)
	}

	updateErr := errors.New("update failed")
	if err := store.Update(ctx, original.ID, func(record *state.Record) error {
		record.UpdatedAt = record.UpdatedAt.Add(time.Hour)
		record.Values["value"][1] = 'x'

		return updateErr
	}); !errors.Is(err, updateErr) {
		t.Fatalf("failed update = %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := store.Update(canceled, original.ID, func(record *state.Record) error {
		cancel()

		record.UpdatedAt = record.UpdatedAt.Add(time.Hour)
		delete(record.Values, "value")

		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update = %v", err)
	}

	loaded, err := store.Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !loaded.UpdatedAt.Equal(original.UpdatedAt) || string(loaded.Values["value"]) != `"original"` {
		t.Fatalf("failed update changed record: %+v", loaded)
	}

	if err := store.Update(ctx, "missing", func(*state.Record) error {
		t.Error("update ran for a missing session")

		return nil
	}); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing update = %v", err)
	}
}

func TestSessionStoreConcurrentUpdatesAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := openStorage(t, t.TempDir()).SessionStore()

	for _, id := range []string{"first", "second"} {
		if err := store.Save(ctx, state.Record{ID: id}); err != nil {
			t.Fatal(err)
		}
	}

	var updates sync.WaitGroup
	for index := range 24 {
		updates.Go(func() {
			key := fmt.Sprintf("key-%d", index)
			if err := store.Update(ctx, "first", func(record *state.Record) error {
				if record.Values == nil {
					record.Values = make(map[string]json.RawMessage)
				}

				record.Values[key] = json.RawMessage(fmt.Sprintf("%d", index))

				return nil
			}); err != nil {
				t.Errorf("update %s: %v", key, err)
			}
		})
	}

	updates.Wait()

	first, err := store.Get(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}

	for index := range 24 {
		key := fmt.Sprintf("key-%d", index)
		if got, want := string(first.Values[key]), fmt.Sprintf("%d", index); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	second, err := store.Get(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Values) != 0 {
		t.Fatalf("values leaked to another session: %+v", second.Values)
	}
}

func openStorage(t *testing.T, root string) *Storage {
	t.Helper()

	storage, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return storage
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}

	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s = %o, want %o", path, got, want)
	}
}
