package assistant

import (
	"context"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/token"
)

type ContextReport struct {
	Instructions []ContextInstruction `json:"instructions"`
	Messages     []ContextMessage     `json:"messages"`
	Tools        []ContextTool        `json:"tools"`
}

func (c ContextReport) Tokens() int {
	return c.InstructionTokens() + c.MessageTokens() + c.ToolTokens()
}

func (c ContextReport) InstructionTokens() int {
	total := 0
	for _, instruction := range c.Instructions {
		total += instruction.Tokens
	}

	return total
}

func (c ContextReport) MessageTokens() int {
	total := 0
	for _, message := range c.Messages {
		total += message.Tokens
	}

	return total
}

func (c ContextReport) ToolTokens() int {
	total := 0
	for _, tool := range c.Tools {
		total += tool.Tokens
	}

	return total
}

type ContextInstruction struct {
	Source string `json:"source,omitempty"`
	Text   string `json:"text"`
	Tokens int    `json:"tokens"`
}

type ContextMessage struct {
	kit.Message
	Tokens int `json:"tokens"`
}

type ContextTool struct {
	kit.ToolDefinition
	Tokens int `json:"tokens"`
}

func (a *Assistant) Context(ctx context.Context, sessionID string) (ContextReport, error) {
	opened, err := a.openSession(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}

	built, err := a.agents.Build(ctx, agent.BuildRequest{
		SessionID: opened.ID(),
		State:     opened.State(),
		History:   agent.NewHistory(opened.ID(), "", a.entries, opened),
	})
	if err != nil {
		return ContextReport{}, err
	}

	resolved, err := built.Context(ctx)
	if err != nil {
		return ContextReport{}, err
	}

	var result ContextReport

	for _, instruction := range resolved.Instructions {
		section := kit.AgentContext{Instructions: []kit.Instruction{instruction}}
		text := section.InstructionsText()
		result.Instructions = append(result.Instructions, ContextInstruction{
			Source: instruction.Source,
			Text:   text,
			Tokens: a.estimator.Estimate(text),
		})
	}

	for _, message := range resolved.Messages {
		result.Messages = append(result.Messages, ContextMessage{
			Message: message,
			Tokens:  token.EstimateMessage(a.estimator, message),
		})
	}

	for _, tool := range resolved.Tools {
		definition := tool.Definition()
		result.Tools = append(result.Tools, ContextTool{
			ToolDefinition: definition,
			Tokens:         token.EstimateTool(a.estimator, definition),
		})
	}

	return result, nil
}
