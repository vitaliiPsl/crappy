package model

import (
	"context"
	"testing"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/retry"
)

func TestRetryMiddlewareRecoversFromRateLimit(t *testing.T) {
	t.Parallel()

	flaky := &flakyModel{failures: 2}
	registry := staticRegistry(t, flaky)

	resolver := registry.Resolver("provider", "model", Retry(retry.Config{
		Budget:      time.Minute,
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}))

	resolved, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	response, err := resolved.Generate(context.Background(), kit.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := flaky.calls, 3; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}

	if got, want := response.Message.TextContent().Text, "recovered"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

type flakyModel struct {
	failures int
	calls    int
}

func (m *flakyModel) Config() kit.ModelConfig { return kit.ModelConfig{} }

func (m *flakyModel) Generate(
	ctx context.Context,
	request kit.ModelRequest,
) (kit.ModelResponse, error) {
	return m.Stream(ctx, request).Result()
}

func (m *flakyModel) Stream(
	_ context.Context,
	_ kit.ModelRequest,
) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.NewStream(func(kit.Emitter[kit.ModelEvent]) (kit.ModelResponse, error) {
		m.calls++
		if m.calls <= m.failures {
			return kit.ModelResponse{}, kit.ErrRateLimit
		}

		return kit.ModelResponse{
			Message:      kit.NewModelMessage(kit.NewTextContent("recovered")),
			FinishReason: kit.FinishReasonStop,
		}, nil
	})
}
