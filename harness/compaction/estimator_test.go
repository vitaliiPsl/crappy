package compaction

import (
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestCharacterEstimatorEstimatesRequestContent(t *testing.T) {
	t.Parallel()

	estimator := CharacterEstimator{}
	short := kit.ModelRequest{
		Messages: []kit.Message{
			kit.NewUserMessage(kit.NewTextContent("short")),
		},
	}
	long := short
	long.Instructions = "additional instructions"
	long.Tools = []kit.Tool{
		kittest.NewTool(t, "search", "Search for relevant information"),
	}

	if got := estimator.EstimateRequest(short); got <= 0 {
		t.Fatalf("EstimateRequest() = %d, want positive estimate", got)
	}

	if got, baseline := estimator.EstimateRequest(long), estimator.EstimateRequest(short); got <= baseline {
		t.Fatalf("long estimate = %d, want greater than %d", got, baseline)
	}
}

func TestCharacterEstimatorEstimatesMessage(t *testing.T) {
	t.Parallel()

	estimator := CharacterEstimator{}
	short := kit.NewUserMessage(kit.NewTextContent("short"))
	long := kit.NewUserMessage(kit.NewTextContent("a considerably longer message"))

	if got, baseline := estimator.EstimateMessage(long), estimator.EstimateMessage(short); got <= baseline {
		t.Fatalf("long estimate = %d, want greater than %d", got, baseline)
	}
}
