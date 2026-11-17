package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
)

func TestSessionUsesInjectedInteractionBroker(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"respond", "close"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			entries := entry.NewMemoryStore()
			events := event.NewBroker("session", entries)
			interactions := interaction.NewBroker(context.Background(), "session", events)

			opened, err := New(ctx, "session", newSessionState(t, "session"), entries, events, interactions, execution.NewRegistry())
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()

			subscription, err := opened.Subscribe(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer subscription.Close()

			finished := make(chan error, 1)
			go func() {
				answer, err := opened.Ask(ctx, "execution", interaction.Question{Text: "Which deadline?"})
				if err == nil && answer.Text != "Friday" {
					t.Errorf("answer = %+v", answer)
				}

				finished <- err
			}()

			value := receiveEvent(t, subscription)

			var request interaction.Request

			if value.Type != interaction.RequestedEntryType {
				t.Fatalf("unexpected event: %+v", value)
			}

			if err := json.Unmarshal(value.Payload, &request); err != nil {
				t.Fatal(err)
			}

			if pending, err := interactions.Pending(ctx); err != nil || len(pending) != 1 || pending[0].ID != request.ID {
				t.Fatalf("injected broker pending = %+v, %v", pending, err)
			}

			if pending, err := opened.PendingPrompts(ctx); err != nil || len(pending) != 1 || pending[0].ID != request.ID {
				t.Fatalf("session pending = %+v, %v", pending, err)
			}

			if action == "close" {
				opened.Close()

				if err := <-finished; !errors.Is(err, interaction.ErrClosed) {
					t.Fatalf("closed question = %v", err)
				}

				if _, err := interactions.Pending(ctx); !errors.Is(err, interaction.ErrClosed) {
					t.Fatalf("session left injected broker open: %v", err)
				}

				recorded, err := entries.List(ctx, "session", 0)
				if err != nil || len(recorded) != 2 || recorded[1].Type != interaction.CancelledEntryType {
					t.Fatalf("shutdown journal = %+v, %v", recorded, err)
				}

				return
			}

			if err := opened.Respond(ctx, request.ID, interaction.Answer{Text: "Friday"}); err != nil {
				t.Fatal(err)
			}

			if err := <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}
