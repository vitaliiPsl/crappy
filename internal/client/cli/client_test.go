package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestClientRunsSession(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "he", "hello"} {
		t.Run("streamed="+text, func(t *testing.T) {
			var events []kit.ModelEvent
			if text != "" {
				events = append(events, kit.NewModelContentDeltaEvent(kit.NewTextContent(text)))
			}

			model := kittest.NewModel(t, kittest.ModelResult{
				Events: events,
				Response: kit.ModelResponse{
					Message:      kit.NewModelMessage(kit.NewTextContent("hello")),
					FinishReason: kit.FinishReasonStop,
				},
			})

			storage, err := jsonl.Open(context.Background(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			h := harness.New(storage, harness.WithModel(staticModelResolver{model}))
			t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

			var output bytes.Buffer

			err = New(
				assistant.New(h),
				"",
				strings.NewReader("hi\n"),
				&output,
			).Run(context.Background())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if got := output.String(); strings.Count(got, "hello") != 1 {
				t.Fatalf("output = %q, want exactly one complete agent response", got)
			}
		})
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
