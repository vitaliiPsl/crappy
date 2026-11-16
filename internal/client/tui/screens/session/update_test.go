package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
)

func TestSubscriptionFailureIsVisibleWhileExecutionIsActive(t *testing.T) {
	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	screen.applyEvent(executionEvent(t, execution.EntryType, "execution-1", execution.Record{Status: execution.Running}))
	screen.Update(eventMsg{target: screen, open: false, err: event.ErrSubscriptionLagged})

	if status := screen.statusView(); !strings.Contains(status, event.ErrSubscriptionLagged.Error()) {
		t.Fatalf("status = %q, want subscription failure", status)
	}

	if !screen.running() {
		t.Fatal("losing a subscription marked the execution as finished")
	}
}

func TestClosedEventBrokerClearsActiveExecution(t *testing.T) {
	screen := New(context.Background(), nil, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	screen.applyEvent(executionEvent(t, execution.EntryType, "execution-1", execution.Record{Status: execution.Running}))
	screen.Update(eventMsg{target: screen, open: false, err: event.ErrClosed})

	if screen.running() {
		t.Fatal("closed session still appears active")
	}

	if status := screen.statusView(); !strings.Contains(status, event.ErrClosed.Error()) {
		t.Fatalf("status = %q, want closed session error", status)
	}
}

func TestContextResultIgnoresOtherAndClosedScreens(t *testing.T) {
	screen := New(context.Background(), nil, "session-1")
	screen.contextLoading = true
	screen.Update(contextMsg{sessionID: "session-1"})

	if !screen.contextLoading {
		t.Fatal("another screen's context result was accepted")
	}

	screen.Close()
	screen.Update(contextMsg{target: screen, sessionID: "session-1"})

	if !screen.contextLoading {
		t.Fatal("a closed screen accepted a context result")
	}
}

func TestSteeringFailurePreservesDraftAndAllowsRetry(t *testing.T) {
	screen := New(t.Context(), nil, "session")
	defer screen.Close()

	screen.applyExecution(execution.Record{ID: "active", Status: execution.Running})
	screen.composer.Focus()
	screen.Update(tea.KeyPressMsg{Code: 'd', Text: "draft"})

	key := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	if _, submit := screen.Update(key); submit == nil {
		t.Fatal("steering was not submitted")
	}

	failure := errors.New("steering rejected")
	screen.Update(executeResultMsg{target: screen, input: "draft", err: failure})

	if screen.composer.Value() != "draft" || screen.submission != "" || screen.notice.message != failure.Error() {
		t.Fatal("steering failure lost the draft, blocked submission, or hid the error")
	}

	if _, retry := screen.Update(key); retry == nil {
		t.Fatal("steering could not be retried")
	}
}
