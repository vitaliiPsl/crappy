package jsonl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vitaliipsl/crappy/harness/session/state"
)

func TestPutBlobPersistsSessionOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	storage := openStorage(t, root)
	if err := storage.SessionStore().Save(ctx, state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	data := []byte("complete command output\n")

	path, err := storage.PutBlob(ctx, "session", data)
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)

	want := filepath.Join(root, "session", blobsDirectory, hex.EncodeToString(sum[:]))
	if path != want {
		t.Fatalf("blob path = %q, want %q", path, want)
	}

	storage = openStorage(t, root)

	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(data) {
		t.Fatalf("stored output = %q, error = %v", stored, err)
	}

	again, err := storage.PutBlob(ctx, "session", data)
	if err != nil || again != path {
		t.Fatalf("repeated output path = %q, error = %v", again, err)
	}

	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("blob files = %v, error = %v; want one content-addressed file", files, err)
	}

	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
}

func TestPutBlobDoesNotRecreateDeletedSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	storage := openStorage(t, root)
	if err := storage.SessionStore().Save(ctx, state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	path, err := storage.PutBlob(ctx, "session", []byte("output"))
	if err != nil {
		t.Fatal(err)
	}

	if err := storage.SessionStore().Delete(ctx, "session"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output survived session deletion: %v", err)
	}

	if _, err := storage.PutBlob(ctx, "session", []byte("late output")); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("write after session deletion = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "session")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output write recreated deleted session: %v", err)
	}
}

func TestPutBlobRejectsMissingInvalidAndCancelledSessions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	root := t.TempDir()

	storage := openStorage(t, root)
	if _, err := storage.PutBlob(ctx, "session", []byte("output")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write = %v", err)
	}

	for _, id := range []string{"", "..", "../outside", "a/b", `a\b`, "missing"} {
		if _, err := storage.PutBlob(context.Background(), id, []byte("output")); err == nil {
			t.Fatalf("PutBlob(%q) succeeded", id)
		}
	}

	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("rejected writes created files: %v, %v", files, err)
	}
}
