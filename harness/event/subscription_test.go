package event

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func TestSubscriptionReplaysEntriesBeforeLiveEvents(t *testing.T) {
	t.Parallel()

	subscription := NewSubscription(context.Background())
	t.Cleanup(subscription.Close)

	stored := entry.Entry{ID: 1, SessionID: "session-1"}

	if !subscription.Publish(Event{Type: "agent.content_delta", Payload: json.RawMessage(`"during replay"`)}) {
		t.Fatal("Publish() closed subscription for transient event")
	}

	if !subscription.Publish(FromEntry(stored)) {
		t.Fatal("Publish() closed subscription for durable event")
	}

	subscription.Start(0, []entry.Entry{stored})

	if !subscription.Publish(Event{Type: "agent.content_delta", Payload: json.RawMessage(`"live"`)}) {
		t.Fatal("Publish() closed subscription for live event")
	}

	first := receiveEvent(t, subscription)
	if first.EntryID != 1 {
		t.Fatalf("first event = %#v, want entry 1", first)
	}

	second := receiveEvent(t, subscription)
	if second.EntryID != 0 || string(second.Payload) != `"live"` {
		t.Fatalf("second event = %#v, want live event", second)
	}
}

func TestSubscriptionClose(t *testing.T) {
	t.Parallel()

	subscription := NewSubscription(context.Background())

	subscription.Close()
	subscription.Close()

	select {
	case _, ok := <-subscription.Events():
		if ok {
			t.Fatal("Events() remained open after Close()")
		}
	case <-time.After(time.Second):
		t.Fatal("Events() did not close")
	}

	if err := subscription.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

func TestSubscriptionClosesWhenDurableBufferFills(t *testing.T) {
	t.Parallel()

	subscription := NewSubscription(context.Background())

	for id := uint64(1); id <= subscriptionBuffer; id++ {
		if !subscription.Publish(Event{EntryID: id}) {
			t.Fatalf("Publish(entry %d) closed subscription", id)
		}
	}

	if subscription.Publish(Event{EntryID: subscriptionBuffer + 1}) {
		t.Fatal("Publish() kept lagging subscription open")
	}

	if !errors.Is(subscription.Err(), ErrSubscriptionLagged) {
		t.Fatalf("Err() = %v, want ErrSubscriptionLagged", subscription.Err())
	}
}

func receiveEvent(t *testing.T, subscription *Subscription) Event {
	t.Helper()

	select {
	case event, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")

		return Event{}
	}
}
