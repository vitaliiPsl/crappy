package token

import (
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestEstimateRequestWithCharacterEstimator(t *testing.T) {
	t.Parallel()

	estimator := NewCharacterEstimator()
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

	if got := EstimateRequest(estimator, short); got <= 0 {
		t.Fatalf("EstimateRequest() = %d, want positive estimate", got)
	}

	if got, baseline := EstimateRequest(estimator, long), EstimateRequest(estimator, short); got <= baseline {
		t.Fatalf("long estimate = %d, want greater than %d", got, baseline)
	}
}

func TestEstimateMessageWithCharacterEstimator(t *testing.T) {
	t.Parallel()

	estimator := NewCharacterEstimator()
	short := kit.NewUserMessage(kit.NewTextContent("short"))
	long := kit.NewUserMessage(kit.NewTextContent("a considerably longer message"))

	if got, baseline := EstimateMessage(estimator, long), EstimateMessage(estimator, short); got <= baseline {
		t.Fatalf("long estimate = %d, want greater than %d", got, baseline)
	}
}

func TestCharacterEstimatorCountsUnicodeCodePoints(t *testing.T) {
	t.Parallel()

	estimator := NewCharacterEstimator()
	for _, test := range []struct {
		text string
		want int
	}{
		{text: "", want: 0},
		{text: "a", want: 1},
		{text: "abcd", want: 1},
		{text: "abcde", want: 2},
		{text: "汉字测试", want: 1},
		{text: "🙂🙂🙂🙂🙂", want: 2},
	} {
		if got := estimator.Estimate(test.text); got != test.want {
			t.Fatalf("Estimate(%q) = %d, want %d", test.text, got, test.want)
		}
	}
}

func TestEstimateRequestUsesCustomTextStrategyForAllInputs(t *testing.T) {
	t.Parallel()

	estimator := estimatorFunc(func(text string) int {
		return strings.Count(text, "needle")
	})
	request := kit.ModelRequest{
		Instructions: "needle",
		Messages:     []kit.Message{kit.NewUserMessage(kit.NewTextContent("needle"))},
		Tools:        []kit.Tool{kittest.NewTool(t, "lookup", "needle")},
		OutputSchema: &kit.OutputSchema{Name: "result", Description: "needle", Schema: map[string]any{"type": "object"}},
	}

	if got := EstimateRequest(estimator, request); got != 4 {
		t.Fatalf("request estimate = %d, want custom counts for instructions, message, tool and output schema", got)
	}
}

func TestEstimateToolIncludesSchema(t *testing.T) {
	t.Parallel()

	estimator := NewCharacterEstimator()
	short := kit.ToolDefinition{Name: "lookup"}
	long := short
	long.Schema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": strings.Repeat("detailed guidance ", 20)},
		},
	}

	if got, baseline := EstimateTool(estimator, long), EstimateTool(estimator, short); got <= baseline {
		t.Fatalf("larger schema estimate = %d, want greater than %d", got, baseline)
	}
}

type estimatorFunc func(string) int

func (fn estimatorFunc) Estimate(text string) int {
	return fn(text)
}
