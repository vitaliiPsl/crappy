package session

import (
	"context"
	"testing"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestFirstSubmitCreatesSession(t *testing.T) {
	storage, err := jsonl.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	screen := New(context.Background(), h, config.Config{}, "")
	defer screen.Close()

	created := screen.submit("fix the compaction")().(createdMsg)
	screen.handleCreated(created)

	if screen.sessionID == "" || screen.sessionID != created.sessionID {
		t.Fatalf("sessionID = %q, want created session %q", screen.sessionID, created.sessionID)
	}

	sessions, err := storage.SessionStore().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 || sessions[0].ID != screen.sessionID {
		t.Fatalf("List() = %#v, want created session", sessions)
	}
}
