package assistant

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/vitaliipsl/crappy/internal/config"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestGetSessionReadsStateWithoutConstructingRuntime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := storage.SessionStore().Save(ctx, state.Record{ID: "saved"}); err != nil {
		t.Fatal(err)
	}

	before, err := storage.EntryStore().Append(ctx, "saved", entry.Entry{
		ExecutionID: "unfinished", Type: execution.StartedEntryType, Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	if record, err := app.GetSession(ctx, "saved"); err != nil || record.ID != "saved" {
		t.Fatalf("session query = %+v, %v", record, err)
	}

	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	if record, err := app.GetSession(ctx, "saved"); err != nil || record.ID != "saved" {
		t.Fatalf("state query depends on host lifetime: %+v, %v", record, err)
	}

	after, err := storage.EntryStore().List(ctx, "saved", 0)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("session query changed execution history: %+v, %v", after, err)
	}
}

func TestSessionPersistsWorkdirAtCreationAndPreservesItOnResume(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	t.Chdir(t.TempDir())

	firstDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	first, err := app.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, first, firstDirectory)

	t.Chdir(t.TempDir())

	secondDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	resumed, err := app.GetSession(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, resumed, firstDirectory)

	second, err := app.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, second, secondDirectory)
	assertSessionWorkdir(t, first, firstDirectory)

	updatedDirectory := t.TempDir()

	opened, err := h.Session(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := opened.State().Set(ctx, workdir.StateKey, updatedDirectory); err != nil {
		t.Fatal(err)
	}

	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	storage, err = jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	restarted := harness.New(storage)
	t.Cleanup(func() { _ = restarted.Shutdown(ctx) })

	app = New(config.Config{}, restarted, storage, nil, token.NewCharacterEstimator())

	for _, test := range []struct {
		id      string
		workdir string
	}{
		{id: first.ID, workdir: updatedDirectory},
		{id: second.ID, workdir: secondDirectory},
	} {
		record, err := app.GetSession(ctx, test.id)
		if err != nil {
			t.Fatal(err)
		}

		assertSessionWorkdir(t, record, test.workdir)
	}
}

func assertSessionWorkdir(t *testing.T, record state.Record, want string) {
	t.Helper()

	var directory string

	if err := json.Unmarshal(record.Values[workdir.StateKey], &directory); err != nil {
		t.Fatal(err)
	}

	if directory != want {
		t.Fatalf("session workdir = %q, want %q", directory, want)
	}
}
