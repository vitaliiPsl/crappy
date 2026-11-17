package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
)

func TestBrokerAnswersAndRecordsInteraction(t *testing.T) {
	t.Parallel()

	store := entry.NewMemoryStore()

	events := event.NewBroker("session", store)
	defer events.Close()

	broker := NewBroker(t.Context(), "session", events)
	defer broker.Close()

	subscription, err := events.Subscribe(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	answer := Answer{Text: "Friday"}

	finished := make(chan error, 1)
	go func() {
		got, err := broker.Ask(t.Context(), "execution", Question{Text: "Which deadline?"})
		if err == nil && got != answer {
			t.Errorf("answer = %+v, want %+v", got, answer)
		}

		finished <- err
	}()

	request := receiveRequest(t, subscription)
	if requests, err := broker.Pending(t.Context()); err != nil || !reflect.DeepEqual(requests, []Request{request}) {
		t.Fatalf("pending = %+v, %v", requests, err)
	}

	subscription.Close()

	subscription, err = events.Subscribe(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	if replayed := receiveRequest(t, subscription); !reflect.DeepEqual(replayed, request) {
		t.Fatalf("replayed request = %+v, want %+v", replayed, request)
	}

	if err := broker.Respond(t.Context(), request.ID, answer); err != nil {
		t.Fatal(err)
	}

	if err := <-finished; err != nil {
		t.Fatal(err)
	}

	if err := broker.Respond(t.Context(), request.ID, Answer{Text: "Monday"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second answer = %v, want ErrNotFound", err)
	}

	if requests, err := broker.Pending(t.Context()); err != nil || len(requests) != 0 {
		t.Fatalf("resolved pending = %+v, %v", requests, err)
	}

	entries, err := store.List(t.Context(), "session", 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}

	var response Response
	if err := json.Unmarshal(entries[1].Payload, &response); err != nil {
		t.Fatal(err)
	}

	if entries[1].Type != AnsweredEntryType || entries[1].ExecutionID != "execution" || response.RequestID != request.ID || response.Answer != answer {
		t.Fatalf("recorded response = %+v, entry = %+v", response, entries[1])
	}
}

func TestBrokerCancellation(t *testing.T) {
	t.Parallel()

	for _, lifetime := range []string{"caller", "session"} {
		t.Run(lifetime, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			owner, caller := t.Context(), ctx

			wantErr := context.Canceled
			if lifetime == "session" {
				owner, caller, wantErr = ctx, t.Context(), ErrClosed
			}

			store := entry.NewMemoryStore()

			events := event.NewBroker("session", store)
			defer events.Close()

			broker := NewBroker(owner, "session", events)
			defer broker.Close()

			subscription, err := events.Subscribe(t.Context(), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer subscription.Close()

			finished := make(chan error, 1)
			go func() {
				_, err := broker.Ask(caller, "execution", Question{Text: "Continue?"})
				finished <- err
			}()

			request := receiveRequest(t, subscription)

			cancel()

			if err := <-finished; !errors.Is(err, wantErr) {
				t.Fatalf("Ask() = %v, want %v", err, wantErr)
			}

			entries, err := store.List(t.Context(), "session", 0)
			if err != nil || len(entries) != 2 || entries[1].Type != CancelledEntryType {
				t.Fatalf("cancelled entries = %+v, %v", entries, err)
			}

			if err := broker.Respond(t.Context(), request.ID, Answer{Text: "too late"}); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrClosed) {
				t.Fatalf("late answer = %v", err)
			}
		})
	}
}

func TestBrokerKeepsRequestPendingWhenResponseCannotBeRecorded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	events := event.NewBroker("session", entry.NewMemoryStore())
	defer events.Close()

	writeErr := errors.New("disk full")
	publisher := &controlledPublisher{Publisher: events, failType: AnsweredEntryType, err: writeErr}

	broker := NewBroker(ctx, "session", publisher)
	defer broker.Close()

	subscription, err := events.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	finished := make(chan error, 1)
	go func() {
		_, err := broker.Ask(ctx, "execution", Question{Text: "Continue?"})
		finished <- err
	}()

	request := receiveRequest(t, subscription)
	if err := broker.Respond(ctx, request.ID, Answer{Text: "yes"}); !errors.Is(err, writeErr) {
		t.Fatalf("failed answer = %v, want write failure", err)
	}

	if requests, err := broker.Pending(ctx); err != nil || len(requests) != 1 {
		t.Fatalf("pending after write failure = %+v, %v", requests, err)
	}

	publisher.failType = ""

	if err := broker.Respond(ctx, request.ID, Answer{Text: "yes"}); err != nil {
		t.Fatal(err)
	}

	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestBrokerCommittedResponseWinsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	events := event.NewBroker("session", entry.NewMemoryStore())
	defer events.Close()

	publisher := &controlledPublisher{Publisher: events, onAnswer: cancel}

	broker := NewBroker(t.Context(), "session", publisher)
	defer broker.Close()

	subscription, err := events.Subscribe(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	finished := make(chan error, 1)
	go func() {
		answer, err := broker.Ask(ctx, "execution", Question{Text: "Continue?"})
		if answer.Text != "yes" {
			t.Errorf("answer = %+v, want the committed response", answer)
		}

		finished <- err
	}()

	request := receiveRequest(t, subscription)
	if err := broker.Respond(t.Context(), request.ID, Answer{Text: "yes"}); err != nil {
		t.Fatal(err)
	}

	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

type controlledPublisher struct {
	event.Publisher
	failType string
	err      error
	onAnswer func()
}

func (p *controlledPublisher) Commit(ctx context.Context, executionID, entryType string, payloads ...any) error {
	if entryType == p.failType {
		return p.err
	}

	if err := p.Publisher.Commit(ctx, executionID, entryType, payloads...); err != nil {
		return err
	}

	if entryType == AnsweredEntryType && p.onAnswer != nil {
		p.onAnswer()
	}

	return nil
}

func receiveRequest(t *testing.T, subscription event.Subscription) Request {
	t.Helper()

	select {
	case value, open := <-subscription.Events():
		if !open {
			t.Fatalf("subscription closed while waiting for a request: %v", subscription.Err())
		}

		if value.Type != RequestedEntryType {
			t.Fatalf("event type = %q, want a request", value.Type)
		}

		var request Request
		if err := json.Unmarshal(value.Payload, &request); err != nil {
			t.Fatal(err)
		}

		return request
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for interaction")

		return Request{}
	}
}
