package memory

import (
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"

	corememory "github.com/vitaliipsl/crappy/memory"
)

type listInput struct {
	Scope   string `json:"scope" jsonschema:"Namespace to browse: personal or workdir"`
	AfterID string `json:"after_id,omitempty" jsonschema:"The next_id returned by the previous catalog page"`
}

type getInput struct {
	ID string `json:"id" jsonschema:"Memory entry ID from the catalog"`
}

type createInput struct {
	Scope   string `json:"scope" jsonschema:"personal for user preferences across workdirs; workdir for knowledge specific to the current directory"`
	Content string `json:"content" jsonschema:"Focused knowledge to remember across sessions"`
}

type updateInput struct {
	ID              string `json:"id" jsonschema:"Memory entry ID"`
	ExpectedVersion uint64 `json:"expected_version" jsonschema:"Current version returned by memory_get"`
	Content         string `json:"content" jsonschema:"Complete replacement content"`
}

type deleteInput struct {
	ID              string `json:"id" jsonschema:"Memory entry ID"`
	ExpectedVersion uint64 `json:"expected_version" jsonschema:"Current version returned by memory_get"`
}

func NewTools(memory *corememory.Memory, directory, source string) []kit.Tool {
	return []kit.Tool{
		tool.MustNew("memory_list", "Browse a page of memory previews in ID order. Follow next_id with after_id to continue; use memory_get for full contents.",
			func(run *kit.RunContext, input listInput) (catalogPage, error) {
				scope, err := resolveScope(input.Scope, directory)
				if err != nil {
					return catalogPage{}, err
				}

				return listCatalog(run.Context, memory, scope, input.AfterID)
			}),
		tool.MustNew("memory_get", "Read a complete memory entry, including its current version, source, and metadata.",
			func(run *kit.RunContext, input getInput) (corememory.Entry, error) {
				return memory.Get(run.Context, input.ID)
			}),
		tool.MustNew("memory_create", "Remember durable knowledge when the user asks. Check existing entries first to avoid duplicates.",
			func(run *kit.RunContext, input createInput) (corememory.Entry, error) {
				scope, err := resolveScope(input.Scope, directory)
				if err != nil {
					return corememory.Entry{}, err
				}

				return memory.Create(run.Context, scope, input.Content, corememory.WithSource(source))
			}),
		tool.MustNew("memory_update", "Correct an existing memory using the version read by memory_get. Read again on a version conflict.",
			func(run *kit.RunContext, input updateInput) (corememory.Entry, error) {
				return memory.Update(run.Context, input.ID, input.ExpectedVersion, input.Content, corememory.WithSource(source))
			}),
		tool.MustNew("memory_delete", "Forget a memory using the version read by memory_get. Read again on a version conflict.",
			func(run *kit.RunContext, input deleteInput) (string, error) {
				if err := memory.Delete(run.Context, input.ID, input.ExpectedVersion); err != nil {
					return "", err
				}

				return "Memory deleted", nil
			}),
	}
}
