package session

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/config"
)

func TestSubscriptionFailureIsVisibleWhileExecutionIsActive(t *testing.T) {
	screen := New(context.Background(), nil, config.Config{}, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	screen.applyEvent(executionEvent(t, execution.StartedEntryType, "execution-1", execution.Started{}))
	screen.Update(eventMsg{target: screen, open: false, err: event.ErrSubscriptionLagged})

	if status := screen.statusView(); !strings.Contains(status, event.ErrSubscriptionLagged.Error()) {
		t.Fatalf("status = %q, want subscription failure", status)
	}

	if !screen.running() {
		t.Fatal("losing a subscription marked the execution as finished")
	}
}

func TestClosedEventBusClearsActiveExecution(t *testing.T) {
	screen := New(context.Background(), nil, config.Config{}, "session-1")
	defer screen.Close()

	screen.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	screen.applyEvent(executionEvent(t, execution.StartedEntryType, "execution-1", execution.Started{}))
	screen.Update(eventMsg{target: screen, open: false, err: event.ErrClosed})

	if screen.running() {
		t.Fatal("closed session still appears active")
	}

	if status := screen.statusView(); !strings.Contains(status, event.ErrClosed.Error()) {
		t.Fatalf("status = %q, want closed session error", status)
	}
}
