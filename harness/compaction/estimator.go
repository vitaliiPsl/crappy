package compaction

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

const charactersPerToken = 4

type Estimator interface {
	EstimateRequest(kit.ModelRequest) int
	EstimateMessage(kit.Message) int
}

type CharacterEstimator struct{}

func (e *CharacterEstimator) EstimateRequest(request kit.ModelRequest) int {
	tokens := textTokens(request.Instructions)
	for _, message := range request.Messages {
		tokens += e.EstimateMessage(message)
	}

	for _, tool := range request.Tools {
		tokens += valueTokens(tool.Definition())
	}

	if request.OutputSchema != nil {
		tokens += valueTokens(request.OutputSchema)
	}

	return tokens
}

func (e *CharacterEstimator) EstimateMessage(message kit.Message) int {
	return valueTokens(message)
}

func valueTokens(value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}

	return characterTokens(utf8.RuneCount(data))
}

func textTokens(value string) int {
	return characterTokens(utf8.RuneCountInString(value))
}

func characterTokens(characters int) int {
	return (characters + charactersPerToken - 1) / charactersPerToken
}
