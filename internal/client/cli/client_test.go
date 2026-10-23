package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestClientRunsSession(t *testing.T) {
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

	storage, err := jsonl.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage, harness.WithModel(staticModelResolver{model}))
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	var output bytes.Buffer

	err = New(
		h,
		"",
		strings.NewReader("hi\n"),
		&output,
	).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := output.String(); !strings.Contains(got, "hello") {
		t.Fatalf("output = %q, want agent response", got)
	}
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}
