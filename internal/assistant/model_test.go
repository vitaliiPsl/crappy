package assistant

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestGetModelReturnsSerializableSelectionWithoutASession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	app := New(config.Config{
		ProviderID:   "provider",
		ModelID:      "model",
		Generation:   config.Generation{Thinking: "high"},
		Instructions: "Internal instructions",
	}, h, storage, nil, token.NewCharacterEstimator())

	data, err := json.Marshal(app.GetModel())
	if err != nil {
		t.Fatal(err)
	}

	if want := `{"provider_id":"provider","model_id":"model","thinking":"high"}`; string(data) != want {
		t.Fatalf("model response = %s, want %s", data, want)
	}

	if sessions, err := app.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("model query created sessions: %+v, %v", sessions, err)
	}
}
