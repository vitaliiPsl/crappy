package checkpoint

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestGeneratorGenerate(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{
		response: checkpointResponse("  current state  ", kit.Usage{
			InputTokens:  10,
			OutputTokens: 2,
		}),
	}}}

	generator, err := NewGenerator(
		staticModelResolver{model},
		WithPrompt("summarize"),
		WithMaxOutputTokens(512),
	)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), []kit.Message{
		kit.NewUserMessage(
			kit.NewTextContent("build a harness"),
		),
		kit.NewModelMessage(
			kit.NewThinkingContent("thinking-1", "private reasoning", "signature"),
			kit.NewTextContent("start with sessions"),
		),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := checkpoint.Summary, "current state"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}

	if got, want := checkpoint.Usage, (kit.Usage{InputTokens: 10, OutputTokens: 2}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Usage = %#v, want %#v", got, want)
	}

	request := model.requests[0]
	if got, want := request.Instructions, "summarize"; got != want {
		t.Fatalf("Instructions = %q, want %q", got, want)
	}

	if request.Config.MaxOutputTokens == nil || *request.Config.MaxOutputTokens != 512 {
		t.Fatalf("MaxOutputTokens = %v, want 512", request.Config.MaxOutputTokens)
	}

	input := request.Messages[0].TextContent().Text
	if !strings.Contains(input, "user:\nbuild a harness") ||
		!strings.Contains(input, "model:\nstart with sessions") {
		t.Fatalf("input = %q", input)
	}

	if strings.Contains(input, "private reasoning") {
		t.Fatalf("input contains discarded content: %q", input)
	}
}

func TestGeneratorChunksAtModelTurnBoundaries(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{
		{err: kit.ErrContextLength},
		{response: checkpointResponse("first checkpoint", kit.Usage{InputTokens: 10, OutputTokens: 2})},
		{response: checkpointResponse("final checkpoint", kit.Usage{InputTokens: 8, OutputTokens: 2})},
	}}
	call := kit.NewToolCall("call-1", "search", map[string]any{"query": "session"})
	result := kit.NewToolResult(call, kit.NewToolOutput(kit.NewTextContent("found")), nil)
	messages := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("one")),
		kit.NewModelMessage(kit.NewTextContent("response one")),
		kit.NewUserMessage(kit.NewTextContent("two")),
		kit.NewModelMessage(kit.NewToolCallContent(call)),
		kit.NewToolMessage(kit.NewToolResultContent(result)),
		kit.NewUserMessage(kit.NewTextContent("three")),
		kit.NewModelMessage(kit.NewTextContent("response three")),
		kit.NewUserMessage(kit.NewTextContent("four")),
		kit.NewModelMessage(kit.NewTextContent("response four")),
	}

	resolveCalls := 0

	generator, err := NewGenerator(modelResolverFunc(func(
		_ context.Context,
	) (kit.Model, error) {
		resolveCalls++

		return model, nil
	}))
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := checkpoint.Summary, "final checkpoint"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}

	if got, want := checkpoint.Usage, (kit.Usage{InputTokens: 18, OutputTokens: 4}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Usage = %#v, want %#v", got, want)
	}

	if got, want := len(model.requests), 3; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}

	if got, want := resolveCalls, 1; got != want {
		t.Fatalf("Resolve() calls = %d, want %d", got, want)
	}

	firstChunk := model.requests[1].Messages[0].TextContent().Text
	if !strings.Contains(firstChunk, "[Tool result from search]\nfound") {
		t.Fatalf("first chunk omitted tool result:\n%s", firstChunk)
	}

	if strings.Contains(firstChunk, "response three") {
		t.Fatalf("first chunk crossed its model-turn boundary:\n%s", firstChunk)
	}

	secondChunk := model.requests[2].Messages[0].TextContent().Text
	if !strings.Contains(secondChunk, "<previous_checkpoint>\nfirst checkpoint") ||
		!strings.Contains(secondChunk, "response four") {
		t.Fatalf("second chunk input:\n%s", secondChunk)
	}
}

func TestGeneratorReturnsContextOverflowForSingleTurn(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{err: kit.ErrContextLength}}}

	generator, err := NewGenerator(staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	_, err = generator.Generate(context.Background(), []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("one")),
		kit.NewModelMessage(kit.NewTextContent("response one")),
	})
	if !errors.Is(err, ErrContextOverflow) {
		t.Fatalf("Generate() error = %v, want ErrContextOverflow", err)
	}
}

func TestGeneratorShrinksRejectedChunks(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{
		{err: kit.ErrContextLength},
		{err: kit.ErrContextLength},
		{response: checkpointResponse("one", kit.Usage{InputTokens: 1})},
		{response: checkpointResponse("two", kit.Usage{InputTokens: 1})},
		{response: checkpointResponse("three", kit.Usage{InputTokens: 1})},
		{response: checkpointResponse("four", kit.Usage{InputTokens: 1})},
	}}

	generator, err := NewGenerator(staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	var messages []kit.Message
	for _, value := range []string{"one", "two", "three", "four"} {
		messages = append(
			messages,
			kit.NewUserMessage(kit.NewTextContent("user "+value)),
			kit.NewModelMessage(kit.NewTextContent("model "+value)),
		)
	}

	checkpoint, err := generator.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := len(model.requests), 6; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}

	if got, want := checkpoint.Summary, "four"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}

	if got, want := checkpoint.Usage.InputTokens, int64(4); got != want {
		t.Fatalf("Usage.InputTokens = %d, want %d", got, want)
	}

	for i, request := range model.requests[2:] {
		input := request.Messages[0].TextContent().Text
		if turns := strings.Count(input, "model:\n"); turns != 1 {
			t.Fatalf("successful request %d has %d model turns, want 1", i, turns)
		}
	}
}

func TestGeneratorValidatesInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewGenerator(nil); err == nil {
		t.Fatal("NewGenerator(nil) error = nil")
	}

	if _, err := NewGenerator(
		staticModelResolver{&checkpointModel{}},
	); err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
}

type modelResult struct {
	response kit.ModelResponse
	err      error
}

type checkpointModel struct {
	results  []modelResult
	requests []kit.ModelRequest
}

func (m *checkpointModel) Config() kit.ModelConfig {
	return kit.ModelConfig{ID: "checkpoint"}
}

func (m *checkpointModel) Generate(
	_ context.Context,
	request kit.ModelRequest,
) (kit.ModelResponse, error) {
	m.requests = append(m.requests, request)
	if len(m.results) == 0 {
		return kit.ModelResponse{}, errors.New("unexpected model call")
	}

	result := m.results[0]
	m.results = m.results[1:]

	return result.response, result.err
}

func (m *checkpointModel) Stream(
	ctx context.Context,
	request kit.ModelRequest,
) *kit.Stream[kit.ModelEvent, kit.ModelResponse] {
	return kit.NewStream(func(kit.Emitter[kit.ModelEvent]) (kit.ModelResponse, error) {
		return m.Generate(ctx, request)
	})
}

type staticModelResolver struct {
	model kit.Model
}

func (r staticModelResolver) Resolve(context.Context) (kit.Model, error) {
	return r.model, nil
}

type modelResolverFunc func(context.Context) (kit.Model, error)

func (fn modelResolverFunc) Resolve(ctx context.Context) (kit.Model, error) {
	return fn(ctx)
}

func checkpointResponse(summary string, usage kit.Usage) kit.ModelResponse {
	return kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(summary)),
		FinishReason: kit.FinishReasonStop,
		Usage:        usage,
	}
}
