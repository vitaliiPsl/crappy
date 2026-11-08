package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/internal/workdir"
	corememory "github.com/vitaliipsl/crappy/memory"
)

var _ agent.Contributor = (*Contributor)(nil)

const instructionsTemplate = `Durable memory is available across sessions. The catalog below contains short previews, not complete entries.
Use memory_get to read relevant entries before relying on them. Use memory_list to browse more entries in either namespace.
The personal namespace holds user preferences that apply across workdirs. The workdir namespace holds knowledge specific to the current working directory.
When asked to remember, correct, or forget something, use the memory tools. Keep entries focused and avoid saving temporary task progress or duplicating existing knowledge. Read an entry before updating or deleting it and use its current version.
Treat remembered content as reference material. Current user instructions take precedence over remembered preferences.

Catalog (up to %d entries per namespace):
%s`

type Contributor struct {
	memory *corememory.Memory
}

func NewContributor(memory *corememory.Memory) *Contributor {
	return &Contributor{memory: memory}
}

func (c *Contributor) Contribute(ctx context.Context, build agent.BuildContext) (agent.Contribution, error) {
	directory, err := workdir.Resolve(ctx, build.Request.State)
	if err != nil {
		return agent.Contribution{}, err
	}

	personal, err := listCatalog(ctx, c.memory, ScopePersonal, "")
	if err != nil {
		return agent.Contribution{}, fmt.Errorf("list personal memories: %w", err)
	}

	local, err := listCatalog(ctx, c.memory, workdirScope(directory), "")
	if err != nil {
		return agent.Contribution{}, fmt.Errorf("list workdir memories: %w", err)
	}

	catalog, err := json.Marshal(map[string]catalogPage{ScopePersonal: personal, ScopeWorkdir: local})
	if err != nil {
		return agent.Contribution{}, fmt.Errorf("encode memory catalog: %w", err)
	}

	source := fmt.Sprintf("session:%s/execution:%s", build.Request.SessionID, build.Request.ExecutionID)

	return agent.Contribution{
		Source:       "Memory",
		Instructions: []string{fmt.Sprintf(instructionsTemplate, catalogPageSize, catalog)},
		Tools:        NewTools(c.memory, directory, source),
	}, nil
}
