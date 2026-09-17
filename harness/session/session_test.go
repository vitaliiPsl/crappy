package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	harnessevent "github.com/vitaliipsl/crappy/harness/event"
)

func TestSessionID(t *testing.T) {
	t.Parallel()

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got, want := session.ID(), "session-1"; got != want {
		t.Fatalf("ID() = %q, want %q", got, want)
	}
}

func TestSessionEntries(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(context.Background(),
		entry.Entry{
			SessionID: "session-1",
			Type:      "message",
			Payload:   json.RawMessage(`{"text":"first"}`),
		},
		entry.Entry{
			SessionID: "session-1",
			Type:      "message",
			Payload:   json.RawMessage(`{"text":"second"}`),
		},
		entry.Entry{
			SessionID: "session-2",
			Type:      "message",
			Payload:   json.RawMessage(`{"text":"other"}`),
		},
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session, err := New(context.Background(), State{ID: "session-1"}, store, nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entries, err := session.Entries(context.Background(), 1)
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Entries() returned %d entries, want 1", len(entries))
	}

	if got, want := entries[0].ID, uint64(2); got != want {
		t.Fatalf("entry ID = %d, want %d", got, want)
	}
}

func TestSubscribeReplaysEntries(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	_, err := store.Append(context.Background(), entry.Entry{
		SessionID: "session-1",
		Type:      "test.entry",
		Payload:   json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	session, err := New(context.Background(), State{ID: "session-1"}, store, nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	event := receiveEvent(t, subscription)
	if event.EntryID != 1 {
		t.Fatalf("replayed event = %#v, want entry 1", event)
	}
}

func TestEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("first Subscribe() error = %v", err)
	}

	t.Cleanup(first.Close)

	second, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}

	t.Cleanup(second.Close)

	if err := session.Emit(
		context.Background(),
		"execution-1",
		"test.event",
		map[string]int{"value": 1},
	); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	for _, subscription := range []*harnessevent.Subscription{first, second} {
		received := receiveEvent(t, subscription)
		if received.SessionID != "session-1" {
			t.Fatalf("SessionID = %q, want %q", received.SessionID, "session-1")
		}

		if received.ExecutionID != "execution-1" {
			t.Fatalf("ExecutionID = %q, want %q", received.ExecutionID, "execution-1")
		}

		if received.EntryID != 0 || received.Type != "test.event" {
			t.Fatalf("event = %#v, want transient test.event", received)
		}
	}
}

func TestExecutePublishesEntriesAndAgentEvents(t *testing.T) {
	t.Parallel()

	model := kittest.NewModel(t, kittest.ModelResult{
		Events: []kit.ModelEvent{
			kit.NewModelContentDeltaEvent(kit.NewTextContent("hello")),
		},
		Response: kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
			FinishReason: kit.FinishReasonStop,
		},
	})

	session, err := New(
		context.Background(),
		State{ID: "session-1"},
		entry.NewMemoryStore(),
		agent.New(kit.AgentConfig{}, staticModelResolver{model}),
		nil,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	subscription, err := session.Subscribe(context.Background(), 0)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	t.Cleanup(subscription.Close)

	executionID, err := session.Execute(kit.NewUserMessage(kit.NewTextContent("hi")))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	events := make([]harnessevent.Event, 6)
	for i := range events {
		events[i] = receiveEvent(t, subscription)
	}

	if events[0].EntryID != 1 || events[0].Type != ExecutionStartedEntryType {
		t.Fatalf("first event = %#v, want execution started entry 1", events[0])
	}

	if events[1].EntryID != 2 || events[1].Type != "message" {
		t.Fatalf("second event = %#v, want message entry 2", events[1])
	}

	if events[2].EntryID != 0 || events[2].Type != "agent.content_delta" {
		t.Fatalf("third event = %#v, want transient content delta", events[2])
	}

	var agentEvent kit.AgentEvent
	if err := json.Unmarshal(events[2].Payload, &agentEvent); err != nil {
		t.Fatalf("decode content delta: %v", err)
	}

	if agentEvent.Content == nil ||
		agentEvent.Content.Text == nil ||
		agentEvent.Content.Text.Text != "hello" {
		t.Fatalf("content delta = %#v, want hello", agentEvent)
	}

	if events[3].EntryID != 3 || events[3].Type != "message" {
		t.Fatalf("fourth event = %#v, want message entry 3", events[3])
	}

	if events[4].EntryID != 0 || events[4].Type != "agent.message" {
		t.Fatalf("fifth event = %#v, want transient agent message", events[4])
	}

	if events[5].EntryID != 4 || events[5].Type != ExecutionFinishedEntryType {
		t.Fatalf("sixth event = %#v, want execution finished entry 4", events[5])
	}

	for _, event := range events {
		if event.ExecutionID != executionID {
			t.Fatalf("ExecutionID = %q, want %q", event.ExecutionID, executionID)
		}
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

func receiveEvent(t *testing.T, subscription *harnessevent.Subscription) harnessevent.Event {
	t.Helper()

	select {
	case event, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")

		return harnessevent.Event{}
	}
}
