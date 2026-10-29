package assistant

import (
	"context"
	"os"
	"testing"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestSessionPersistsWorkdirAtCreationAndPreservesItOnResume(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(h, storage.SessionStore())

	t.Chdir(t.TempDir())

	firstDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if directory, err := app.Workdir(ctx, ""); err != nil || directory != firstDirectory {
		t.Fatalf("new conversation workdir = %q, error = %v; want %q", directory, err, firstDirectory)
	}

	if records, err := storage.SessionStore().List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("workdir lookup created a session: records = %v, error = %v", records, err)
	}

	first, err := app.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, first, firstDirectory)

	t.Chdir(t.TempDir())

	secondDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	resumed, err := app.Session(ctx, first.ID())
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, resumed, firstDirectory)

	if directory, err := app.Workdir(ctx, first.ID()); err != nil || directory != firstDirectory {
		t.Fatalf("resumed workdir = %q, error = %v; want %q", directory, err, firstDirectory)
	}

	second, err := app.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	assertSessionWorkdir(t, second, secondDirectory)
	assertSessionWorkdir(t, first, firstDirectory)

	updatedDirectory := t.TempDir()
	if err := first.State().Set(ctx, workdir.StateKey, updatedDirectory); err != nil {
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

	app = New(restarted, storage.SessionStore())

	for _, test := range []struct {
		id      string
		workdir string
	}{
		{id: first.ID(), workdir: updatedDirectory},
		{id: second.ID(), workdir: secondDirectory},
	} {
		opened, err := app.Session(ctx, test.id)
		if err != nil {
			t.Fatal(err)
		}

		assertSessionWorkdir(t, opened, test.workdir)
	}
}

func assertSessionWorkdir(t *testing.T, opened *session.Session, want string) {
	t.Helper()

	var directory string

	found, err := opened.State().Get(context.Background(), workdir.StateKey, &directory)
	if err != nil || !found || directory != want {
		t.Fatalf("session %s workdir = %q, found = %v, error = %v; want %q", opened.ID(), directory, found, err, want)
	}
}
