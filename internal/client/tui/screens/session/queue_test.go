package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestQueueTracksSelectionAndKeepsCancelledRequestsOutOfTranscript(t *testing.T) {
	screen := New(context.Background(), nil, "session")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	for _, record := range []execution.Record{
		{ID: "first", Status: execution.Queued},
		{ID: "first", Status: execution.Running},
		{ID: "second", Status: execution.Queued},
		{ID: "third", Status: execution.Queued},
	} {
		screen.applyExecution(record)
	}

	screen.showQueue()
	before := screen.transcript.View()
	screen.applyExecution(execution.Record{ID: "second", Status: execution.Cancelled})

	if !screen.running() || screen.queueEntries()[screen.queueSelected].ID != "third" {
		t.Fatal("queue cancellation changed the active execution or lost selection")
	}

	if status := strings.TrimSpace(ansi.Strip(screen.statusView())); status != "Working… · 1 queued" {
		t.Fatalf("running status = %q", status)
	}

	if after := screen.transcript.View(); after != before {
		t.Fatalf("queued cancellation changed the transcript: %q", after)
	}

	screen.applyExecution(execution.Record{ID: "first", Status: execution.Interrupted})

	if screen.queueEntries()[screen.queueSelected].ID != "third" {
		t.Fatal("interruption moved selection to another request")
	}

	if status := strings.TrimSpace(ansi.Strip(screen.statusView())); status != "Interrupted · 1 queued · /resume to continue" {
		t.Fatalf("restored status = %q", status)
	}
}

func TestQueuePreservesDraftAndEscapeReturnsToComposer(t *testing.T) {
	screen := New(context.Background(), nil, "session")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	screen.composer.Focus()
	screen.Update(tea.KeyPressMsg{Code: 'd', Text: "draft"})
	screen.applyExecution(execution.Record{ID: "active", Status: execution.Running})
	screen.applyExecution(execution.Record{ID: "next", Status: execution.Queued})
	screen.showQueue()
	screen.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if screen.queueOpen || !screen.running() || screen.composer.Value() != "draft" {
		t.Fatalf("queue navigation changed work or draft: open=%v, running=%v, draft=%q", screen.queueOpen, screen.running(), screen.composer.Value())
	}
}

func TestRestoredQueueWithoutInterruptedWorkOffersResume(t *testing.T) {
	screen := New(context.Background(), nil, "session")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	for _, id := range []string{"first", "second", "third"} {
		screen.applyExecution(execution.Record{ID: id, Status: execution.Queued})
	}

	if status := strings.TrimSpace(ansi.Strip(screen.statusView())); status != "3 queued · /resume to start" {
		t.Fatalf("restored queued status = %q", status)
	}

	if screen.running() || !strings.Contains(screen.shortcutsView(), "Queue") {
		t.Fatal("restored queued work is shown as running or blocks new submissions")
	}
}

func TestAdmissionPreservesNewerDraftAndAllowsRetryAfterFailure(t *testing.T) {
	screen := New(context.Background(), nil, "session")
	defer screen.Close()

	screen.composer.Focus()
	screen.Update(tea.KeyPressMsg{Code: 'f', Text: "first"})

	if command := screen.submit("first"); command == nil {
		t.Fatal("message was not submitted")
	}

	screen.Update(tea.KeyPressMsg{Code: ' ', Text: " and a new draft"})
	screen.handleExecuteResult(executeResultMsg{target: screen, executionID: "first", input: "first"})

	if screen.composer.Value() != "first and a new draft" || screen.submission != "" {
		t.Fatal("late admission cleared a newer draft or kept submission blocked")
	}

	text := screen.composer.Value()
	if command := screen.submit(text); command == nil {
		t.Fatal("next draft was not submitted")
	}

	failure := errors.New("request rejected")
	screen.handleExecuteResult(executeResultMsg{target: screen, input: text, err: failure})

	if screen.composer.Value() != text || screen.notice.message != failure.Error() {
		t.Fatal("rejected admission lost the draft or its error")
	}

	if command := screen.submit(text); command == nil {
		t.Fatal("rejected submission could not be retried")
	}

	screen.handleExecuteResult(executeResultMsg{target: screen, executionID: "next", input: text})

	if screen.composer.Value() != "" {
		t.Fatal("accepted draft was not cleared")
	}
}

func TestQueueViewFitsWindowAndKeepsSelectedRequestVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {80, 20}, {120, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			screen := New(context.Background(), nil, "session")
			defer screen.Close()

			screen.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

			for i := range 30 {
				input, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(fmt.Sprintf("task-%02d\n%s", i, strings.Repeat("long ", 30)))))
				if err != nil {
					t.Fatal(err)
				}

				status := execution.Queued
				if i == 0 {
					status = execution.Interrupted
				}

				screen.applyExecution(execution.Record{ID: fmt.Sprint(i), Executor: agent.ExecutorName, Input: input, Status: status})
			}

			screen.showQueue()
			screen.moveQueue(29)

			view := screen.View().Content
			t.Logf("Queue view:\n%s", ansi.Strip(view))

			if width, height := lipgloss.Width(view), lipgloss.Height(view); width != size[0] || height != size[1] {
				t.Fatalf("queue view = %dx%d, want %dx%d: %q", width, height, size[0], size[1], view)
			}

			if !strings.Contains(view, "task-29") || !strings.Contains(view, "Pending work") || !strings.Contains(view, "…") {
				t.Fatalf("selected request or truncated preview is missing: %q", view)
			}

			screen.Update(tea.WindowSizeMsg{Width: 40, Height: 16})

			if view := screen.View().Content; !strings.Contains(view, "task-29") {
				t.Fatal("resizing hid the selected request")
			}

			screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

			view = screen.View().Content
			if width, height := lipgloss.Width(view), lipgloss.Height(view); width != 40 || height != 16 {
				t.Fatalf("collapsed queue view = %dx%d, want 40x16", width, height)
			}

			if !strings.Contains(view, "/resume") || strings.Contains(view, "task-29") {
				t.Fatalf("collapsed queue status is missing or previews escaped the list: %q", view)
			}
		})
	}
}

func TestQueueRestoresResumesAndCancelsThroughAssistant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := t.TempDir()

	storage, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	original := harness.New(storage)

	opened, err := original.CreateSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i, id := range []string{"first", "second"} {
		input, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(id + " task")))
		if err != nil {
			t.Fatal(err)
		}

		status := execution.Queued
		if i == 0 {
			status = execution.Running
		}

		if err := opened.Commit(ctx, id, execution.EntryType, execution.Record{ID: id, Executor: agent.ExecutorName, Input: input, Status: status}); err != nil {
			t.Fatal(err)
		}
	}

	if err := original.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	storage, err = jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan string, 2)
	h := harness.New(storage, harness.WithExecutor(agent.ExecutorName, execution.Func(
		func(ctx context.Context, params execution.ExecuteParams) error {
			started <- params.ExecutionID

			<-ctx.Done()

			return ctx.Err()
		},
	)))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := assistant.New(config.Config{}, h, storage, nil, token.NewCharacterEstimator())

	screen := New(ctx, app, opened.ID())
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	_, next := screen.Update(screen.subscribe()())
	for range 3 {
		_, next = screen.Update(next())
	}

	if status := strings.TrimSpace(ansi.Strip(screen.statusView())); status != "Interrupted · 1 queued · /resume to continue" {
		t.Fatalf("restored status = %q", status)
	}

	accepted := screen.submit("/resume")().(executeResultMsg)
	if accepted.err != nil || accepted.executionID != "first" {
		t.Fatalf("resume = %+v", accepted)
	}

	_, wait := screen.Update(accepted)

	select {
	case id := <-started:
		if id != "first" {
			t.Fatalf("unexpected execution: %s", id)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	_, next = screen.Update(next())
	screen.submit("/queue")

	if !strings.Contains(screen.View().Content, "second task") {
		t.Fatal("queue is missing the restored request preview")
	}

	command := screen.handleQueueKey("d")
	screen.Update(command())

	_, next = screen.Update(next())
	if len(screen.queueEntries()) != 0 || !screen.running() {
		t.Fatal("cancelling queued work changed the running execution")
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, command = screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	screen.Update(command())
	screen.Update(wait())
	screen.Update(next())

	if screen.running() || len(screen.pending) != 0 {
		t.Fatal("cancelled executions remain pending")
	}

	if status, err := app.GetExecutionStatus(ctx, opened.ID(), "second"); err != nil || status != execution.Cancelled {
		t.Fatalf("queued cancellation was not persisted: %s, %v", status, err)
	}

	select {
	case id := <-started:
		t.Fatalf("cancelled request ran: %s", id)
	default:
	}
}
