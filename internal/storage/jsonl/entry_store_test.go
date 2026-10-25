package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestEntryStorePersistsEntries(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)
	store := storage.EntryStore()

	appended, err := store.Append(
		context.Background(),
		"session-1",
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        "user.message",
			Payload:     json.RawMessage(`{"text":"hello"}`),
		},
		entry.Entry{
			ExecutionID: "execution-1",
			Type:        "assistant.message",
			Payload:     json.RawMessage(`{"text":"hi"}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if len(appended) != 2 || appended[0].ID != 1 || appended[1].ID != 2 {
		t.Fatalf("Append() = %#v, want IDs 1 and 2", appended)
	}

	path := filepath.Join(root, "session-1", transcriptFilename)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got, want := bytes.Count(data, []byte{'\n'}), 2; got != want {
		t.Fatalf("transcript lines = %d, want %d", got, want)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})

	var first entry.Entry
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("first line is not an entry: %v", err)
	}

	if first.ID != 1 || first.Type != "user.message" {
		t.Fatalf("first line = %#v, want first entry", first)
	}

	if !bytes.Contains(data, []byte(`"text":"hello"`)) {
		t.Fatalf("transcript does not expose payload text: %s", data)
	}

	storage = openStorage(t, root)
	store = storage.EntryStore()

	listed, err := store.List(context.Background(), "session-1", 1)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 1 {
		t.Fatalf("List() returned %d entries, want 1", len(listed))
	}

	got := listed[0]
	if got.ID != 2 || got.ExecutionID != "execution-1" || got.Type != "assistant.message" {
		t.Fatalf("List() = %#v, want persisted second entry", got)
	}

	if string(got.Payload) != `{"text":"hi"}` || got.CreatedAt.IsZero() {
		t.Fatalf("List() = %#v, want complete persisted entry", got)
	}

	appended, err = store.Append(context.Background(), "session-1", entry.Entry{
		Type:    "compaction",
		Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Append() after reopen error = %v", err)
	}

	if got, want := appended[0].ID, uint64(3); got != want {
		t.Fatalf("ID after reopen = %d, want %d", got, want)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after append error = %v", err)
	}

	if !bytes.HasPrefix(updated, data) {
		t.Fatal("Append() changed existing transcript bytes")
	}
}

func TestEntryStoreKeepsSessionsSeparate(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)
	store := storage.EntryStore()

	for _, sessionID := range []string{"session-1", "session-2"} {
		appended, err := store.Append(context.Background(), sessionID, entry.Entry{
			Type: "test.entry",
		})
		if err != nil {
			t.Fatalf("Append(%s) error = %v", sessionID, err)
		}

		if got, want := appended[0].ID, uint64(1); got != want {
			t.Fatalf("Append(%s) ID = %d, want %d", sessionID, got, want)
		}
	}
}

func TestEntryStoreConcurrentAppend(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))
	store := storage.EntryStore()

	const count = 50

	var wait sync.WaitGroup
	for range count {
		wait.Add(1)

		go func() {
			defer wait.Done()

			if _, err := store.Append(context.Background(), "session-1", entry.Entry{
				Type: "test.entry",
			}); err != nil {
				t.Errorf("Append() error = %v", err)
			}
		}()
	}

	wait.Wait()

	listed, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != count {
		t.Fatalf("List() returned %d entries, want %d", len(listed), count)
	}

	for index, current := range listed {
		if got, want := current.ID, uint64(index+1); got != want {
			t.Fatalf("entry %d ID = %d, want %d", index, got, want)
		}
	}
}

func TestEntryStoreRepairsIncompleteTail(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)
	store := storage.EntryStore()

	if _, err := store.Append(context.Background(), "session-1", entry.Entry{
		Type: "first",
	}); err != nil {
		t.Fatalf("first Append() error = %v", err)
	}

	path := filepath.Join(root, "session-1", transcriptFilename)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	if _, err := file.WriteString(`{"id":`); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	storage = openStorage(t, root)
	store = storage.EntryStore()

	listed, err := store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() with incomplete tail error = %v", err)
	}

	if len(listed) != 1 {
		t.Fatalf("List() returned %d entries, want 1", len(listed))
	}

	appended, err := store.Append(context.Background(), "session-1", entry.Entry{
		Type: "second",
	})
	if err != nil {
		t.Fatalf("second Append() error = %v", err)
	}

	if got, want := appended[0].ID, uint64(2); got != want {
		t.Fatalf("recovered entry ID = %d, want %d", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got, want := bytes.Count(data, []byte{'\n'}), 2; got != want {
		t.Fatalf("transcript lines = %d, want %d: %q", got, want, data)
	}

	listed, err = store.List(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("List() after repair error = %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("List() after repair returned %d entries, want 2", len(listed))
	}
}

func TestEntryStoreRejectsCorruptCompleteRecord(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	storage := openStorage(t, root)
	store := storage.EntryStore()

	if _, err := store.Append(context.Background(), "session-1", entry.Entry{
		Type: "first",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	path := filepath.Join(root, "session-1", transcriptFilename)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	storage = openStorage(t, root)
	store = storage.EntryStore()

	if _, err := store.List(context.Background(), "session-1", 0); err == nil {
		t.Fatal("List() error = nil, want transcript corruption")
	}
}

func TestEntryStoreHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	storage := openStorage(t, filepath.Join(t.TempDir(), "sessions"))
	store := storage.EntryStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Append(ctx, "session-1", entry.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append() error = %v, want context.Canceled", err)
	}

	if _, err := store.List(ctx, "session-1", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}
}

func TestEntryStoreLatestAndListAfterAcrossReopen(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sessions")
	store := openStorage(t, root).EntryStore()

	if _, err := store.Append(context.Background(), "session-1",
		entry.Entry{Type: "message", Payload: json.RawMessage(`1`)},
		entry.Entry{Type: "compaction", Payload: json.RawMessage(`2`)},
		entry.Entry{Type: "message", Payload: json.RawMessage(`3`)},
	); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if _, err := store.Append(context.Background(), "session-1",
		entry.Entry{Type: "message", Payload: json.RawMessage(`4`)},
	); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	for name, current := range map[string]entry.Store{
		"same store": store,
		"reopened":   openStorage(t, root).EntryStore(),
	} {
		latest, found, err := current.Latest(context.Background(), "session-1", "compaction")
		if err != nil || !found || latest.ID != 2 {
			t.Fatalf("%s: Latest() = %#v, %v, %v, want entry 2", name, latest, found, err)
		}

		after, err := current.List(context.Background(), "session-1", latest.ID)
		if err != nil {
			t.Fatalf("%s: List() error = %v", name, err)
		}

		if len(after) != 2 || after[0].ID != 3 || after[1].ID != 4 {
			t.Fatalf("%s: List(after 2) = %#v, want entries 3 and 4", name, after)
		}

		if _, found, err := current.Latest(context.Background(), "session-1", "missing"); err != nil || found {
			t.Fatalf("%s: Latest(missing) = %v, %v, want not found", name, found, err)
		}
	}
}
