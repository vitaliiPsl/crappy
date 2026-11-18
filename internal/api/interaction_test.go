package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestInteractionClientRoutesAnswersToWaitingExecution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	store, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	want := interaction.Answer{Text: "Friday, under $15,000"}
	executor := execution.Func(func(ctx context.Context, params execution.ExecuteParams) error {
		answer, err := params.Interactions.Ask(ctx, params.ExecutionID, interaction.Question{Text: "Which deadline?"})
		if err == nil && answer != want {
			t.Errorf("answer = %+v, want %+v", answer, want)
		}

		return err
	})
	h := harness.New(store, harness.WithExecutor(agent.ExecutorName, executor))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	app := service.NewLocal(config.Config{}, h, store, nil, token.NewCharacterEstimator(), nil, nil)

	handler := NewHandler(app, func() {})
	defer handler.Close()

	client := NewClient("unused", t.TempDir())
	defer client.Close()

	client.http.Transport = &handlerTransport{handler: handler}

	opened, err := client.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := h.Session(ctx, opened.ID)
	if err != nil {
		t.Fatal(err)
	}

	events, err := subscription.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()

	id, err := client.Execute(ctx, opened.ID, kit.NewTextContent("Prepare the report"))
	if err != nil {
		t.Fatal(err)
	}

	var request interaction.Request
	for request.ID == "" {
		select {
		case value := <-events.Events():
			if value.Type == interaction.RequestedEntryType {
				if err := json.Unmarshal(value.Payload, &request); err != nil {
					t.Fatal(err)
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	events.Close()

	if pending, err := client.PendingPrompts(ctx, opened.ID); err != nil || len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("pending after disconnect = %+v, %v", pending, err)
	}

	other, err := client.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := client.Respond(ctx, other.ID, request.ID, want); !errors.Is(err, interaction.ErrNotFound) {
		t.Fatalf("response in another session = %v", err)
	}

	if err := client.Respond(ctx, opened.ID, request.ID, want); err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, opened.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("execution = %s, %v", status, err)
	}

	if err := client.Respond(ctx, opened.ID, request.ID, want); !errors.Is(err, interaction.ErrNotFound) {
		t.Fatalf("duplicate response = %v", err)
	}

	if pending, err := client.PendingPrompts(ctx, opened.ID); err != nil || len(pending) != 0 {
		t.Fatalf("resolved pending = %+v, %v", pending, err)
	}
}

type handlerTransport struct {
	handler http.Handler
}

func (h *handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)

	return recorder.Result(), nil
}
