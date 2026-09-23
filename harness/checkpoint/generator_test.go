package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestGeneratorGenerate(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{
		response: checkpointResponse("current state", kit.Usage{
			InputTokens:  10,
			OutputTokens: 2,
		}),
	}}}

	generator, err := NewGenerator(
		Config{Prompt: "summarize", MaxOutputTokens: 512, RetainedTokens: &retainNothing},
		staticModelResolver{model},
	)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), nil, []kit.Message{
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

	if got, want := checkpoint.Objective, "current state"; got != want {
		t.Fatalf("Objective = %q, want %q", got, want)
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

	if request.OutputSchema == nil || request.OutputSchema.Name != "checkpoint" {
		t.Fatalf("OutputSchema = %#v, want checkpoint schema", request.OutputSchema)
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

	generator, err := NewGenerator(Config{RetainedTokens: &retainNothing}, modelResolverFunc(func(
		_ context.Context,
	) (kit.Model, error) {
		resolveCalls++

		return model, nil
	}))
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), nil, messages)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := checkpoint.Objective, "final checkpoint"; got != want {
		t.Fatalf("Objective = %q, want %q", got, want)
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
	if !strings.Contains(secondChunk, `"objective": "first checkpoint"`) ||
		!strings.Contains(secondChunk, "response four") {
		t.Fatalf("second chunk input:\n%s", secondChunk)
	}
}

func TestGeneratorReturnsContextOverflowForSingleTurn(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{err: kit.ErrContextLength}}}

	generator, err := NewGenerator(Config{RetainedTokens: &retainNothing}, staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	_, err = generator.Generate(context.Background(), nil, []kit.Message{
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

	generator, err := NewGenerator(Config{RetainedTokens: &retainNothing}, staticModelResolver{model})
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

	checkpoint, err := generator.Generate(context.Background(), nil, messages)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := len(model.requests), 6; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}

	if got, want := checkpoint.Objective, "four"; got != want {
		t.Fatalf("Objective = %q, want %q", got, want)
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

func TestGeneratorUpdatesPreviousCheckpoint(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{
		response: checkpointResponse("updated", kit.Usage{}),
	}}}

	generator, err := NewGenerator(Config{RetainedTokens: &retainNothing}, staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	if _, err := generator.Generate(
		context.Background(),
		&Checkpoint{Summary: Summary{Objective: "previous"}},
		[]kit.Message{kit.NewUserMessage(kit.NewTextContent("new work"))},
	); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	input := model.requests[0].Messages[0].TextContent().Text
	if !strings.Contains(input, `"objective": "previous"`) ||
		!strings.Contains(input, "new work") {
		t.Fatalf("input = %q", input)
	}
}

func TestGeneratorKeepFrom(t *testing.T) {
	t.Parallel()

	call := kit.NewToolCall("call-1", "search", map[string]any{})
	result := kit.NewToolResult(call, kit.NewToolOutput(kit.NewTextContent("found")), nil)
	messages := []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("old")),
		kit.NewModelMessage(kit.NewToolCallContent(call)),
		kit.NewToolMessage(kit.NewToolResultContent(result)),
		kit.NewModelMessage(kit.NewTextContent("recent")),
	}

	estimator := CharacterEstimator{}
	recent := estimator.EstimateMessage(messages[3])
	toolResult := estimator.EstimateMessage(messages[2])

	tests := []struct {
		name     string
		retained int
		want     int
	}{
		{name: "nothing fits", retained: 0, want: 4},
		{name: "last message", retained: recent, want: 3},
		{name: "never splits tool result from its call", retained: recent + toolResult, want: 3},
		{name: "always summarizes the first message", retained: 1 << 20, want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			generator, err := NewGenerator(
				Config{RetainedTokens: &test.retained},
				staticModelResolver{&checkpointModel{}},
			)
			if err != nil {
				t.Fatalf("NewGenerator() error = %v", err)
			}

			if got := generator.keepFrom(messages); got != test.want {
				t.Fatalf("keepFrom() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestGeneratorDecodesSummary(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{
		response: textResponse(`{
			"objective": "ship compaction",
			"progress": ["done: retained tail"],
			"context": ["harness/checkpoint/generator.go"],
			"next": ["evaluate checkpoints"]
		}`, kit.Usage{}),
	}}}

	generator, err := NewGenerator(Config{}, staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), nil, []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("work")),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := Summary{
		Objective: "ship compaction",
		Progress:  []string{"done: retained tail"},
		Context:   []string{"harness/checkpoint/generator.go"},
		Next:      []string{"evaluate checkpoints"},
	}
	if !reflect.DeepEqual(checkpoint.Summary, want) {
		t.Fatalf("Summary = %#v, want %#v", checkpoint.Summary, want)
	}
}

func TestGeneratorRejectsInvalidSummary(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"not json":        "plain text",
		"empty objective": `{"objective": " ", "progress": [], "context": [], "next": []}`,
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			model := &checkpointModel{results: []modelResult{{response: textResponse(text, kit.Usage{})}}}

			generator, err := NewGenerator(Config{}, staticModelResolver{model})
			if err != nil {
				t.Fatalf("NewGenerator() error = %v", err)
			}

			if _, err := generator.Generate(context.Background(), nil, []kit.Message{
				kit.NewUserMessage(kit.NewTextContent("work")),
			}); err == nil {
				t.Fatal("Generate() error = nil, want error")
			}
		})
	}
}

func TestGeneratorValidatesInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewGenerator(Config{}, nil); err == nil {
		t.Fatal("NewGenerator(Config{}, nil) error = nil")
	}

	if _, err := NewGenerator(
		Config{},
		staticModelResolver{&checkpointModel{}},
	); err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
}

func TestGeneratorCountsRetainedMessages(t *testing.T) {
	t.Parallel()

	model := &checkpointModel{results: []modelResult{{
		response: checkpointResponse("summary", kit.Usage{}),
	}}}
	retained := 1 << 20

	generator, err := NewGenerator(Config{RetainedTokens: &retained}, staticModelResolver{model})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	checkpoint, err := generator.Generate(context.Background(), nil, []kit.Message{
		kit.NewUserMessage(kit.NewTextContent("summarized")),
		kit.NewModelMessage(kit.NewTextContent("retained")),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got, want := checkpoint.Retained, 1; got != want {
		t.Fatalf("Retained = %d, want %d", got, want)
	}

	input := model.requests[0].Messages[0].TextContent().Text
	if strings.Contains(input, "retained") {
		t.Fatalf("input contains retained message: %q", input)
	}
}

func TestGeneratorRejectsEmptyMessages(t *testing.T) {
	t.Parallel()

	generator, err := NewGenerator(Config{}, staticModelResolver{&checkpointModel{}})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	if _, err := generator.Generate(context.Background(), nil, nil); err == nil {
		t.Fatal("Generate() error = nil, want error")
	}
}

var retainNothing = 0

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

func checkpointResponse(objective string, usage kit.Usage) kit.ModelResponse {
	summary, _ := json.Marshal(Summary{Objective: objective})

	return textResponse(string(summary), usage)
}

func textResponse(text string, usage kit.Usage) kit.ModelResponse {
	return kit.ModelResponse{
		Message:      kit.NewModelMessage(kit.NewTextContent(text)),
		FinishReason: kit.FinishReasonStop,
		Usage:        usage,
	}
}
