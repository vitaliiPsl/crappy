package memory

import (
	"context"
	"strings"

	corememory "github.com/vitaliipsl/crappy-memory/memory"
)

const (
	catalogPageSize = 10
	previewLength   = 160
)

type catalogEntry struct {
	ID      string `json:"id"`
	Preview string `json:"preview"`
	Version uint64 `json:"version"`
}

type catalogPage struct {
	Entries []catalogEntry `json:"entries"`
	NextID  string         `json:"next_id,omitempty"`
}

func listCatalog(ctx context.Context, memory *corememory.Memory, scope, afterID string) (catalogPage, error) {
	page, err := memory.List(ctx, corememory.Filter{
		Scopes: []string{scope}, AfterID: afterID, Limit: catalogPageSize,
	})
	if err != nil {
		return catalogPage{}, err
	}

	catalog := catalogPage{Entries: make([]catalogEntry, 0, len(page.Entries)), NextID: page.NextID}
	for _, entry := range page.Entries {
		preview := []rune(strings.Join(strings.Fields(entry.Content), " "))
		if len(preview) > previewLength {
			preview = append(preview[:previewLength], '…')
		}

		catalog.Entries = append(catalog.Entries, catalogEntry{
			ID: entry.ID, Preview: string(preview), Version: entry.Version,
		})
	}

	return catalog, nil
}
