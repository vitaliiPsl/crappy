package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type Tool struct {
	remoteName string
	definition kit.ToolDefinition
	session    *mcpsdk.ClientSession
}

func NewTool(server string, session *mcpsdk.ClientSession, definition *mcpsdk.Tool) (*Tool, error) {
	name := "mcp__" + server + "__" + definition.Name
	if len(name) > 64 || !toolNamePattern.MatchString(name) {
		return nil, fmt.Errorf("MCP tool name %q must contain only letters, digits, underscores, or hyphens and fit in 64 characters", name)
	}

	data, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("encode schema for %q: %w", name, err)
	}

	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode schema for %q: %w", name, err)
	}

	return &Tool{
		remoteName: definition.Name,
		definition: kit.ToolDefinition{Name: name, Description: definition.Description, Schema: schema},
		session:    session,
	}, nil
}

func (t *Tool) Definition() kit.ToolDefinition {
	return t.definition
}

func (t *Tool) Execute(run *kit.RunContext, call kit.ToolCall) (kit.ToolOutput, error) {
	result, err := t.session.CallTool(run.Context, &mcpsdk.CallToolParams{
		Name: t.remoteName, Arguments: call.Arguments,
	})
	if err != nil {
		return kit.ToolOutput{}, err
	}

	return convertToolResult(result)
}

func discoverTools(ctx context.Context, config ServerConfig, session *mcpsdk.ClientSession) ([]kit.Tool, error) {
	var tools []kit.Tool

	for definition, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}

		if len(config.Tools) > 0 && !slices.Contains(config.Tools, definition.Name) {
			continue
		}

		tool, err := NewTool(config.Name, session, definition)
		if err != nil {
			return nil, err
		}

		tools = append(tools, tool)
	}

	return tools, nil
}

func convertToolResult(result *mcpsdk.CallToolResult) (kit.ToolOutput, error) {
	if result == nil {
		return kit.ToolOutput{}, errors.New("MCP tool returned no result")
	}

	output := kit.ToolOutput{
		Structured: result.StructuredContent,
	}
	for _, content := range result.Content {
		converted, err := convertContent(content)
		if err != nil {
			return output, err
		}

		output.Content = append(output.Content, converted)
	}

	if result.IsError {
		message := kit.ContentsText(output.Content)
		if message == "" {
			message = "MCP tool returned an error"
		}

		return output, errors.New(message)
	}

	return output, nil
}

func convertContent(content mcpsdk.Content) (kit.Content, error) {
	switch value := content.(type) {
	case *mcpsdk.TextContent:
		return kit.NewTextContent(value.Text), nil
	case *mcpsdk.ImageContent:
		return kit.NewImageContent(value.MIMEType, value.Data), nil
	case *mcpsdk.AudioContent:
		return kit.NewAudioContent(value.MIMEType, value.Data), nil
	case *mcpsdk.ResourceLink:
		return kit.NewResourceContent(kit.Resource{
			URI: value.URI, Name: value.Name, Title: value.Title,
			Description: value.Description, MIMEType: value.MIMEType,
		}), nil
	case *mcpsdk.EmbeddedResource:
		resource := value.Resource
		if resource == nil {
			return kit.Content{}, errors.New("MCP tool returned an empty embedded resource")
		}

		return kit.NewResourceContent(kit.Resource{
			URI: resource.URI, MIMEType: resource.MIMEType, Text: resource.Text, Blob: resource.Blob,
		}), nil
	default:
		data, err := json.Marshal(content)
		if err != nil {
			return kit.Content{}, fmt.Errorf("encode MCP content: %w", err)
		}

		return kit.NewTextContent(string(data)), nil
	}
}
