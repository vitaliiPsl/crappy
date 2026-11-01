package contextview

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestInspectorLoadsAndRefreshesWithoutExecuting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)
	agents := agent.New(kit.AgentConfig{}, inspectorModelResolver{model})
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)))
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := assistant.New(h, storage, agents, token.NewCharacterEstimator())

	opened, err := app.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	screen := New(ctx, app, opened.ID())
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen.Update(screen.Init()())

	if screen.loading || screen.err != nil || len(screen.data.Messages) != 0 {
		t.Fatalf("initial snapshot = %+v, error = %v", screen.data, screen.err)
	}

	message := kit.NewUserMessage(kit.NewTextContent("new context"))
	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, message); err != nil {
		t.Fatal(err)
	}

	_, refresh := screen.Update(tea.KeyPressMsg{Code: 'r'})
	if refresh == nil || !screen.loading {
		t.Fatal("refresh did not load a new snapshot")
	}

	if _, repeated := screen.Update(tea.KeyPressMsg{Code: 'r'}); repeated != nil {
		t.Fatal("refresh started concurrent inspections")
	}

	screen.Update(refresh())

	if screen.err != nil || len(screen.data.Messages) != 1 {
		t.Fatalf("refreshed snapshot = %+v, error = %v", screen.data, screen.err)
	}

	entries, err := opened.Entries(ctx, 0)
	if err != nil || len(entries) != 1 || entries[0].Type != agent.MessageEntryType {
		t.Fatalf("inspection created execution entries: %v, %v", entries, err)
	}

	empty := New(ctx, app, "")
	defer empty.Close()

	empty.Update(empty.Init()())

	if !errors.Is(empty.err, assistant.ErrSessionRequired) {
		t.Fatalf("empty-session inspection returned %v", empty.err)
	}

	if sessions, err := app.Sessions(ctx); err != nil || len(sessions) != 1 {
		t.Fatalf("inspection created another session: %v, %v", sessions, err)
	}

	model.AssertNeverCalled(t)
}

type inspectorModelResolver struct {
	model kit.Model
}

func (r inspectorModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
