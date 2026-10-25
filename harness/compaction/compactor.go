package compaction

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/output"
)

var ErrContextOverflow = errors.New("the smallest compaction chunk exceeds the model context")

//go:embed prompt.md
var DefaultPrompt string

const (
	DefaultMaxOutputTokens int32 = 4096
	DefaultRetainedTokens  int   = 20_000
)

type ModelResolver interface {
	Resolve(context.Context) (kit.Model, error)
}

type Config struct {
	Prompt          string
	MaxOutputTokens int32
	RetainedTokens  *int
}

func (c Config) withDefaults() Config {
	c.Prompt = cmp.Or(c.Prompt, DefaultPrompt)
	c.MaxOutputTokens = cmp.Or(c.MaxOutputTokens, DefaultMaxOutputTokens)

	retained := DefaultRetainedTokens
	if c.RetainedTokens != nil {
		retained = *c.RetainedTokens
	}

	c.RetainedTokens = &retained

	return c
}

type CompactorOption func(*Compactor)

func WithEstimator(estimator Estimator) CompactorOption {
	return func(compactor *Compactor) {
		compactor.estimator = estimator
	}
}

type Compactor struct {
	config    Config
	models    ModelResolver
	estimator Estimator
	schema    kit.OutputSchema
}

func NewCompactor(config Config, models ModelResolver, options ...CompactorOption) (*Compactor, error) {
	schema, err := output.For[Summary]("compaction", "Summary of the work so far.")
	if err != nil {
		return nil, err
	}

	compactor := &Compactor{
		config:    config.withDefaults(),
		models:    models,
		estimator: &CharacterEstimator{},
		schema:    schema,
	}

	for _, option := range options {
		option(compactor)
	}

	return compactor, nil
}

func (c *Compactor) Compact(
	ctx context.Context,
	previous *Compaction,
	messages []kit.Message,
) (Compaction, error) {
	if previous == nil && len(messages) == 0 {
		return Compaction{}, errors.New("no messages to compact")
	}

	keepFrom := c.keepFrom(messages)

	compaction, err := c.summarize(ctx, previous, messages[:keepFrom])
	if err != nil {
		return compaction, err
	}

	compaction.Retained = messages[keepFrom:]
	compaction.TokensBefore = c.estimateContext(previous, messages)
	compaction.TokensAfter = c.estimateContext(&compaction, messages[keepFrom:])

	return compaction, nil
}

func (c *Compactor) estimateContext(compaction *Compaction, messages []kit.Message) int {
	tokens := 0
	if compaction != nil {
		tokens += c.estimator.EstimateMessage(compaction.Message())
	}

	for _, message := range messages {
		tokens += c.estimator.EstimateMessage(message)
	}

	return tokens
}

func (c *Compactor) keepFrom(messages []kit.Message) int {
	keepFrom := len(messages)
	tokens := 0

	for i := len(messages) - 1; i > 0; i-- {
		tokens += c.estimator.EstimateMessage(messages[i])
		if tokens > *c.config.RetainedTokens {
			break
		}

		if messages[i].Role != kit.RoleTool {
			keepFrom = i
		}
	}

	return keepFrom
}

func (c *Compactor) summarize(
	ctx context.Context,
	previous *Compaction,
	messages []kit.Message,
) (Compaction, error) {
	resolved, err := c.models.Resolve(ctx)
	if err != nil {
		return Compaction{}, fmt.Errorf("resolve model: %w", err)
	}

	compaction, err := c.generate(ctx, resolved, previous, messages)
	if !errors.Is(err, kit.ErrContextLength) {
		return compaction, err
	}

	chunks, chunkErr := c.generateChunks(ctx, resolved, previous, messages)
	chunks.Usage.Add(compaction.Usage)

	return chunks, chunkErr
}

func (c *Compactor) generate(
	ctx context.Context,
	model kit.Model,
	previous *Compaction,
	messages []kit.Message,
) (Compaction, error) {
	input, err := c.compactionInput(previous, messages)
	if err != nil {
		return Compaction{}, err
	}

	response, err := model.Generate(ctx, kit.ModelRequest{
		Instructions: c.config.Prompt,
		Messages: []kit.Message{
			kit.NewUserMessage(kit.NewTextContent(input)),
		},
		Config: kit.GenerationConfig{
			MaxOutputTokens: &c.config.MaxOutputTokens,
		},
		OutputSchema: &c.schema,
	})
	if err != nil {
		return Compaction{Usage: response.Usage}, fmt.Errorf("generate compaction: %w", err)
	}

	return c.compactionOutput(response)
}

func (c *Compactor) generateChunks(
	ctx context.Context,
	model kit.Model,
	previous *Compaction,
	messages []kit.Message,
) (Compaction, error) {
	chunkTurns := countTurns(messages) / 2
	if chunkTurns == 0 {
		return Compaction{}, ErrContextOverflow
	}

	var (
		result Compaction
		usage  kit.Usage
	)

	for len(messages) > 0 {
		chunk, remaining := firstTurns(messages, chunkTurns)

		generated, err := c.generate(ctx, model, previous, chunk)
		usage.Add(generated.Usage)

		if err == nil {
			result = generated
			previous = &result
			messages = remaining

			continue
		}

		if !errors.Is(err, kit.ErrContextLength) {
			return Compaction{Usage: usage}, err
		}

		chunkTurns = countTurns(chunk) / 2
		if chunkTurns == 0 {
			return Compaction{Usage: usage}, ErrContextOverflow
		}
	}

	result.Usage = usage

	return result, nil
}

func (c *Compactor) compactionInput(previous *Compaction, messages []kit.Message) (string, error) {
	var input strings.Builder

	if previous != nil {
		summary, err := json.MarshalIndent(previous.Summary, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode previous compaction: %w", err)
		}

		input.WriteString("<previous_compaction>\n")
		input.Write(summary)
		input.WriteString("\n</previous_compaction>\n\n")
	}

	input.WriteString("<context>\n")
	input.WriteString(flatten(messages))
	input.WriteString("\n</context>")

	return input.String(), nil
}

func (c *Compactor) compactionOutput(response kit.ModelResponse) (Compaction, error) {
	result := Compaction{Usage: response.Usage}
	switch response.FinishReason {
	case kit.FinishReasonToolCall:
		return result, errors.New("compaction model returned an unexpected tool call")
	case kit.FinishReasonSafety:
		return result, errors.New("compaction model response was blocked by a safety filter")
	case kit.FinishReasonMaxTokens:
		return result, errors.New("compaction model returned a truncated response")
	}

	text := response.Message.TextContent()
	if text == nil || strings.TrimSpace(text.Text) == "" {
		return result, errors.New("compaction model returned an empty response")
	}

	var summary Summary
	if err := json.Unmarshal([]byte(text.Text), &summary); err != nil {
		return result, fmt.Errorf("decode compaction: %w", err)
	}

	if strings.TrimSpace(summary.Objective) == "" {
		return result, errors.New("compaction model returned no objective")
	}

	result.Summary = summary

	return result, nil
}

func countTurns(messages []kit.Message) int {
	turns := 0
	for _, message := range messages {
		if message.Role == kit.RoleModel {
			turns++
		}
	}

	return turns
}

func firstTurns(messages []kit.Message, turns int) (head, tail []kit.Message) {
	seen := 0
	for boundary, message := range messages {
		if message.Role != kit.RoleModel {
			continue
		}

		seen++
		if seen == turns {
			boundary++
			for boundary < len(messages) && messages[boundary].Role == kit.RoleTool {
				boundary++
			}

			return messages[:boundary], messages[boundary:]
		}
	}

	return messages, nil
}
