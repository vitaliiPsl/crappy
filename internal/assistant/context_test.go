package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	adkagent "github.com/vitaliiPsl/crappy-adk/agent"
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
)

func TestContextInspectsEffectiveInputsWithoutExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	model := kittest.NewModel(t)
	tool := kittest.NewTool(t, "lookup", "Search for information")
	agents := agent.New(kit.AgentConfig{Instructions: "Be concise."}, contextModelResolver{model},
		agent.WithToolMiddleware(tools.Timeout(tools.Config{Timeout: time.Second, MaxTimeout: time.Minute})),
		agent.WithContributors(agent.ContributorFunc(func(_ context.Context, build agent.BuildContext) (agent.Contribution, error) {
			if build.Request.ExecutionID != "" {
				t.Fatalf("inspection created an execution ID: %q", build.Request.ExecutionID)
			}

			return agent.Contribution{
				Source:       "Session Instructions",
				Instructions: []string{"Follow local guidance."},
				Tools:        []kit.Tool{tool},
				Options: []adkagent.Option{
					adkagent.WithOnModelRequestReady(func(*kit.RunContext, kit.ModelRequest) (bool, error) {
						t.Fatal("inspection ran automatic compaction hooks")

						return false, nil
					}),
				},
			}, nil
		})),
	)
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)))
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(h, storage, agents, fixedEstimator(23))
	if _, err := app.Context(ctx, ""); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("Context without a session: %v", err)
	}

	if sessions, err := app.Sessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("inspection created a session: %v, %v", sessions, err)
	}

	opened, err := app.Session(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	old := kit.NewUserMessage(kit.NewTextContent("old conversation"))
	retained := kit.NewUserMessage(kit.NewTextContent("retained message"))
	latest := kit.NewModelMessage(kit.NewTextContent("latest response"))
	compacted := compaction.Compaction{Summary: compaction.Summary{Objective: "Continue the task"}, Retained: []kit.Message{retained}}

	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, old); err != nil {
		t.Fatal(err)
	}

	if err := opened.Commit(ctx, "prior", compaction.EntryType, compacted); err != nil {
		t.Fatal(err)
	}

	if err := opened.Commit(ctx, "prior", agent.MessageEntryType, latest); err != nil {
		t.Fatal(err)
	}

	before, err := opened.Entries(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	report, err := app.Context(ctx, opened.ID())
	if err != nil {
		t.Fatal(err)
	}

	if report.Workdir == "" {
		t.Fatalf("incorrect inspection metadata: %+v", report)
	}

	if len(report.Instructions) != 2 || report.Instructions[0].Source != "" || report.Instructions[1].Text != "# Session Instructions\n\nFollow local guidance." {
		t.Fatalf("instruction order or framing changed: %+v", report.Instructions)
	}

	for _, instruction := range report.Instructions {
		if instruction.Tokens != 23 {
			t.Fatalf("instruction did not use the injected estimator: %+v", instruction)
		}
	}

	wantMessages := []kit.Message{compacted.Message(), retained, latest}
	if len(report.Messages) != len(wantMessages) {
		t.Fatalf("effective messages = %+v", report.Messages)
	}

	for index, message := range report.Messages {
		if !reflect.DeepEqual(message.Message, wantMessages[index]) || message.Tokens != 23 {
			t.Fatalf("message %d = %+v", index, message)
		}
	}

	if len(report.Tools) != 1 || report.Tools[0].Name != "lookup" || report.Tools[0].Tokens != 23 {
		t.Fatalf("tool definitions = %+v", report.Tools)
	}

	encoded, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(encoded), "timeout_seconds") {
		t.Fatalf("report omitted decorated schema: %s, %v", encoded, err)
	}

	if report.Tokens() != 138 {
		t.Fatalf("incorrect input estimate: %+v", report)
	}

	after, err := opened.Entries(ctx, 0)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("inspection changed the journal: %v, %v", after, err)
	}

	model.AssertNeverCalled(t)
	tool.AssertNeverCalled(t)
}

type fixedEstimator int

func (e fixedEstimator) Estimate(string) int {
	return int(e)
}

type contextModelResolver struct {
	model kit.Model
}

func (r contextModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
