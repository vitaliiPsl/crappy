package session

import (
	"context"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/runtime"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestFirstSubmitCreatesTitledSession(t *testing.T) {
	storage, err := jsonl.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	model := kittest.NewModel(t, kittest.ModelResult{Response: kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent("done")),
		FinishReason: kit.FinishReasonStop,
	}})
	appRuntime := runtime.New(
		context.Background(),
		storage.SessionStore(),
		storage.EntryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil,
	)

	screen := New(context.Background(), appRuntime, "")
	created := screen.submit("  fix   the compaction ")().(createdMsg)
	screen.handleCreated(created)

	if screen.sessionID == "" || screen.sessionID != created.state.ID {
		t.Fatalf("sessionID = %q, want created session %q", screen.sessionID, created.state.ID)
	}

	sessions, err := appRuntime.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(sessions) != 1 || sessions[0].Title != "fix the compaction" {
		t.Fatalf("List() = %#v, want one titled session", sessions)
	}
}

func TestInputTitleTruncatesLongInput(t *testing.T) {
	title := inputTitle(kit.NewUserMessage(kit.NewTextContent(strings.Repeat("word ", 30))))

	runes := []rune(title)
	if len(runes) > titleLimit+1 || runes[len(runes)-1] != '…' {
		t.Fatalf("inputTitle() = %q, want at most %d runes ending with an ellipsis", title, titleLimit+1)
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
