package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
)

type staticModelResolver struct{ model kit.Model }

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) { return r.model, nil }

func receiveEvent(t *testing.T, subscription *event.Subscription) event.Event {
	t.Helper()

	select {
	case value, ok := <-subscription.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", subscription.Err())
		}

		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")

		return event.Event{}
	}
}

func agentInput(t *testing.T, text string) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(kit.NewUserMessage(kit.NewTextContent(text)))
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}
