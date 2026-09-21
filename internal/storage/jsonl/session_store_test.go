package jsonl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
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

	if len(states) != 2 || states[0].ID != "session-1" || states[1].ID != "session-2" {
		t.Fatalf("List() = %#v, want both sessions in ID order", states)
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

func TestStorageProtectsTranscript(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)

	if _, err := storage.EntryStore().Append(context.Background(), "session-1", entry.Entry{
		Type: "test.entry",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, "session-1"), 0o700)

	path := filepath.Join(root, "session-1", transcriptFilename)
	assertMode(t, path, 0o600)
}

func TestEntryStoreRejectsUnsafeSessionID(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))
	for _, sessionID := range []string{"", ".", "..", "nested/session", `nested\session`} {
		if _, err := storage.EntryStore().Append(
			context.Background(),
			sessionID,
			entry.Entry{},
		); err == nil {
			t.Fatalf("Append(%q) error = nil", sessionID)
		}
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
