package token

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const charactersPerToken = 4

// Estimator estimates tokens in text and must be safe for concurrent use.
type Estimator interface {
	Estimate(text string) int
}

// CharacterEstimator approximates one token per four Unicode code points.
type CharacterEstimator struct{}

var _ Estimator = (*CharacterEstimator)(nil)

func NewCharacterEstimator() *CharacterEstimator {
	return &CharacterEstimator{}
}

func (*CharacterEstimator) Estimate(text string) int {
	characters := utf8.RuneCountInString(text)

	return (characters + charactersPerToken - 1) / charactersPerToken
}

func EstimateTool(estimator Estimator, tool kit.ToolDefinition) int {
	return estimateValue(estimator, tool)
}

func EstimateRequest(estimator Estimator, request kit.ModelRequest) int {
	tokens := estimator.Estimate(request.Instructions)
	for _, message := range request.Messages {
		tokens += EstimateMessage(estimator, message)
	}

	for _, tool := range request.Tools {
		tokens += EstimateTool(estimator, tool.Definition())
	}

	if request.OutputSchema != nil {
		tokens += estimateValue(estimator, request.OutputSchema)
	}

	return tokens
}

func EstimateMessage(estimator Estimator, message kit.Message) int {
	return estimateValue(estimator, message)
}

func estimateValue(estimator Estimator, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}

	return estimator.Estimate(string(data))
}
