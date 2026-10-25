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

type GeneratorOption func(*Generator)

func WithEstimator(estimator Estimator) GeneratorOption {
	return func(generator *Generator) {
		generator.estimator = estimator
	}
}

type Generator struct {
	config    Config
	models    ModelResolver
	estimator Estimator
	schema    kit.OutputSchema
}

func NewGenerator(config Config, models ModelResolver, options ...GeneratorOption) (*Generator, error) {
	schema, err := output.For[Summary]("compaction", "Summary of the work so far.")
	if err != nil {
		return nil, err
	}

	generator := &Generator{
		config:    config.withDefaults(),
		models:    models,
		estimator: &CharacterEstimator{},
		schema:    schema,
	}

	for _, option := range options {
		option(generator)
	}

	return generator, nil
}

func (g *Generator) Generate(
	ctx context.Context,
	previous *Compaction,
	messages []kit.Message,
) (*Compaction, error) {
	if previous == nil && len(messages) == 0 {
		return nil, errors.New("no messages to compact")
	}

	keepFrom := g.keepFrom(messages)

	compaction, err := g.summarize(ctx, previous, messages[:keepFrom])
	if err != nil {
		return compaction, err
	}

	compaction.Retained = messages[keepFrom:]
	compaction.TokensBefore = g.estimateContext(previous, messages)
	compaction.TokensAfter = g.estimateContext(compaction, messages[keepFrom:])

	return compaction, nil
}

func (g *Generator) estimateContext(compaction *Compaction, messages []kit.Message) int {
	tokens := 0
	if compaction != nil {
		tokens += g.estimator.EstimateMessage(compaction.Message())
	}

	for _, message := range messages {
		tokens += g.estimator.EstimateMessage(message)
	}

	return tokens
}

func (g *Generator) keepFrom(messages []kit.Message) int {
	keepFrom := len(messages)
	tokens := 0

	for i := len(messages) - 1; i > 0; i-- {
		tokens += g.estimator.EstimateMessage(messages[i])
		if tokens > *g.config.RetainedTokens {
			break
		}

		if messages[i].Role != kit.RoleTool {
			keepFrom = i
		}
	}

	return keepFrom
}

func (g *Generator) summarize(
	ctx context.Context,
	previous *Compaction,
	messages []kit.Message,
) (*Compaction, error) {
	resolved, err := g.models.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	compaction, err := g.generate(ctx, resolved, previous, messages)
	if !errors.Is(err, kit.ErrContextLength) {
		return compaction, err
	}

	chunks, chunkErr := g.generateChunks(ctx, resolved, previous, messages)
	if compaction != nil {
		if chunks == nil {
			chunks = &Compaction{}
		}
		chunks.Usage.Add(compaction.Usage)
	}
	return chunks, chunkErr
}

func (g *Generator) generate(
	ctx context.Context,
	model kit.Model,
	previous *Compaction,
	messages []kit.Message,
) (*Compaction, error) {
	input, err := g.generatorInput(previous, messages)
	if err != nil {
		return nil, err
	}

	response, err := model.Generate(ctx, kit.ModelRequest{
		Instructions: g.config.Prompt,
		Messages: []kit.Message{
			kit.NewUserMessage(kit.NewTextContent(input)),
		},
		Config: kit.GenerationConfig{
			MaxOutputTokens: &g.config.MaxOutputTokens,
		},
		OutputSchema: &g.schema,
	})
	if err != nil {
		return &Compaction{Usage: response.Usage}, fmt.Errorf("generate compaction: %w", err)
	}

	return g.generatorOutput(response)
}

func (g *Generator) generateChunks(
	ctx context.Context,
	model kit.Model,
	compaction *Compaction,
	messages []kit.Message,
) (*Compaction, error) {
	chunkTurns := countTurns(messages) / 2
	if chunkTurns == 0 {
		return nil, ErrContextOverflow
	}

	var usage kit.Usage

	for len(messages) > 0 {
		chunk, remaining := firstTurns(messages, chunkTurns)

		generated, err := g.generate(ctx, model, compaction, chunk)
		if generated != nil {
			usage.Add(generated.Usage)
		}
		if err == nil {
			generated.Usage = usage
			compaction = generated
			messages = remaining

			continue
		}

		if !errors.Is(err, kit.ErrContextLength) {
			return &Compaction{Usage: usage}, err
		}

		chunkTurns = countTurns(chunk) / 2
		if chunkTurns == 0 {
			return &Compaction{Usage: usage}, ErrContextOverflow
		}
	}

	return compaction, nil
}

func (g *Generator) generatorInput(previous *Compaction, messages []kit.Message) (string, error) {
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

func (g *Generator) generatorOutput(response kit.ModelResponse) (*Compaction, error) {
	result := &Compaction{Usage: response.Usage}
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
