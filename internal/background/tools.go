package background

import (
	"encoding/json"
	"fmt"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

type idInput struct {
	ID string `json:"id" jsonschema:"Background job ID"`
}

func NewTools(manager *Manager, sessionID string) []kit.Tool {
	return []kit.Tool{
		tool.MustNew("job_get", "Get a background job's status and output in this conversation.",
			func(run *kit.RunContext, input idInput) (kit.ToolOutput, error) {
				result, err := manager.Get(run.Context, sessionID, input.ID)
				if err != nil {
					return kit.ToolOutput{}, err
				}

				return resultOutput(result)
			}),
		tool.MustNew("job_list", "List background jobs in this conversation.",
			func(run *kit.RunContext, _ struct{}) ([]Record, error) {
				return manager.List(run.Context, sessionID)
			}),
		tool.MustNew("job_cancel", "Request cancellation of a background job in this conversation.",
			func(run *kit.RunContext, input idInput) (Record, error) {
				return manager.Cancel(run.Context, sessionID, input.ID)
			}),
	}
}

func resultOutput(result Result) (kit.ToolOutput, error) {
	header := fmt.Sprintf("Background job %s (%s): %s", result.ID, result.Tool, result.State)
	if result.Error != "" {
		header += "\nError: " + result.Error
	}

	content := []kit.Content{kit.NewTextContent(header)}
	content = append(content, result.Output.Content...)

	if result.Output.Structured != nil {
		data, err := json.Marshal(result.Output.Structured)
		if err != nil {
			return kit.ToolOutput{}, fmt.Errorf("encode job output: %w", err)
		}

		content = append(content, kit.NewTextContent(string(data)))
	}

	return kit.NewToolOutput(content...), nil
}
