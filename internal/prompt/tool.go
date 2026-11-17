package prompt

import (
	"errors"
	"strings"

	"github.com/vitaliipsl/crappy/harness/interaction"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

const ToolName = "ask_user"

type input struct {
	Question string               `json:"question" jsonschema:"The question and the context needed to answer it"`
	Choices  []interaction.Choice `json:"choices,omitempty" jsonschema:"Optional choices with unique IDs, labels, and optional descriptions; free-text answers are also allowed"`
}

func NewTool(prompter interaction.Prompter, executionID string) kit.Tool {
	return tool.MustNew(ToolName, "Ask the user for information or a decision that materially blocks the work, then wait for their answer.",
		func(run *kit.RunContext, input input) (interaction.Answer, error) {
			if strings.TrimSpace(input.Question) == "" {
				return interaction.Answer{}, errors.New("question must not be empty")
			}

			return prompter.Ask(run.Context, executionID, interaction.Question{Text: input.Question, Choices: input.Choices})
		})
}
