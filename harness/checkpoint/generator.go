package checkpoint

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var ErrContextOverflow = errors.New("the smallest checkpoint chunk exceeds the model context")

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
}

func NewGenerator(config Config, models ModelResolver, options ...GeneratorOption) (*Generator, error) {
	if models == nil {
		return nil, errors.New("model resolver is required")
	}

	generator := &Generator{
		config:    config.withDefaults(),
		models:    models,
		estimator: &CharacterEstimator{},
	}

	for _, option := range options {
		option(generator)
	}

	return generator, nil
}

func (g *Generator) Generate(
	ctx context.Context,
	previous *Checkpoint,
	messages []kit.Message,
) (*Checkpoint, error) {
	if len(messages) == 0 {
		return nil, errors.New("no messages to checkpoint")
	}

	keepFrom := g.keepFrom(messages)

	checkpoint, err := g.summarize(ctx, previous, messages[:keepFrom])
	if err != nil {
		return nil, err
	}

	checkpoint.Retained = len(messages) - keepFrom

	return checkpoint, nil
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
	previous *Checkpoint,
	messages []kit.Message,
) (*Checkpoint, error) {
	resolved, err := g.models.Resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	checkpoint, err := g.generate(ctx, resolved, previous, messages)
	if !errors.Is(err, kit.ErrContextLength) {
		return checkpoint, err
	}

	return g.generateChunks(ctx, resolved, previous, messages)
}

func (g *Generator) generate(
	ctx context.Context,
	model kit.Model,
	previous *Checkpoint,
	messages []kit.Message,
) (*Checkpoint, error) {
	response, err := model.Generate(ctx, kit.ModelRequest{
		Instructions: g.config.Prompt,
		Messages: []kit.Message{
			kit.NewUserMessage(
				kit.NewTextContent(g.generatorInput(previous, messages)),
			),
		},
		Config: kit.GenerationConfig{
			MaxOutputTokens: &g.config.MaxOutputTokens,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate checkpoint: %w", err)
	}

	return g.generatorOutput(response)
}

func (g *Generator) generateChunks(
	ctx context.Context,
	model kit.Model,
	checkpoint *Checkpoint,
	messages []kit.Message,
) (*Checkpoint, error) {
	chunkTurns := countTurns(messages) / 2
	if chunkTurns == 0 {
		return nil, ErrContextOverflow
	}

	var usage kit.Usage

	for len(messages) > 0 {
		chunk, remaining := firstTurns(messages, chunkTurns)

		generated, err := g.generate(ctx, model, checkpoint, chunk)
		if err == nil {
			usage.Add(generated.Usage)
			generated.Usage = usage
			checkpoint = generated
			messages = remaining

			continue
		}

		if !errors.Is(err, kit.ErrContextLength) {
			return nil, err
		}

		chunkTurns = countTurns(chunk) / 2
		if chunkTurns == 0 {
			return nil, ErrContextOverflow
		}
	}

	return checkpoint, nil
}

func (g *Generator) generatorInput(previous *Checkpoint, messages []kit.Message) string {
	var input strings.Builder

	if previous != nil {
		input.WriteString("<previous_checkpoint>\n")
		input.WriteString(previous.Summary)
		input.WriteString("\n</previous_checkpoint>\n\n")
	}

	input.WriteString("<context>\n")
	input.WriteString(flatten(messages))
	input.WriteString("\n</context>")

	return input.String()
}

func (g *Generator) generatorOutput(response kit.ModelResponse) (*Checkpoint, error) {
	switch response.FinishReason {
	case kit.FinishReasonToolCall:
		return nil, errors.New("checkpoint model returned an unexpected tool call")
	case kit.FinishReasonSafety:
		return nil, errors.New("checkpoint model response was blocked by a safety filter")
	case kit.FinishReasonMaxTokens:
		return nil, errors.New("checkpoint model returned a truncated response")
	}

	text := response.Message.TextContent()
	if text == nil || strings.TrimSpace(text.Text) == "" {
		return nil, errors.New("checkpoint model returned an empty response")
	}

	return &Checkpoint{
		Summary: strings.TrimSpace(text.Text),
		Usage:   response.Usage,
	}, nil
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
