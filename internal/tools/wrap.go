package tools

import (
	"maps"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type argument struct {
	name   string
	schema map[string]any
}

type nextTool func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error)

type handler func(
	*kit.RunContext,
	kit.ToolCall,
	nextTool,
) (kit.ToolOutput, error)

type wrappedTool struct {
	tool       kit.Tool
	definition kit.ToolDefinition
	arguments  []argument
	handler    handler
}

func wrap(
	tool kit.Tool,
	arguments []argument,
	handler handler,
) (kit.Tool, error) {
	definition := tool.Definition()

	schema, err := addArguments(definition.Schema, arguments)
	if err != nil {
		return nil, err
	}

	definition.Schema = schema

	return wrappedTool{
		tool:       tool,
		definition: definition,
		arguments:  append([]argument(nil), arguments...),
		handler:    handler,
	}, nil
}

func (t wrappedTool) Definition() kit.ToolDefinition {
	return t.definition
}

func (t wrappedTool) Execute(
	run *kit.RunContext,
	call kit.ToolCall,
) (kit.ToolOutput, error) {
	next := func(run *kit.RunContext, call kit.ToolCall) (kit.ToolOutput, error) {
		call.Arguments = removeArguments(call.Arguments, t.arguments)

		return t.tool.Execute(run, call)
	}

	return t.handler(run, call, next)
}

func removeArguments(values map[string]any, arguments []argument) map[string]any {
	cleaned := maps.Clone(values)
	for _, current := range arguments {
		delete(cleaned, current.name)
	}

	return cleaned
}
