package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	corememory "github.com/vitaliipsl/crappy-memory/memory"
)

func TestCatalogBoundsPreviewsAndPaginatesWithinScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memory := openMemory(t, filepath.Join(t.TempDir(), "memory.json"))

	content := "Remember\n\t" + strings.Repeat("界", 500)
	for range 11 {
		if _, err := memory.Create(ctx, "selected", content, corememory.WithMetadata("extra", "not in catalog")); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := memory.Create(ctx, "other", "Another namespace"); err != nil {
		t.Fatal(err)
	}

	page, err := listCatalog(ctx, memory, "selected", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Entries) != 10 || page.NextID == "" {
		t.Fatalf("first page = %+v", page)
	}

	seen := make(map[string]bool)
	for _, entry := range page.Entries {
		seen[entry.ID] = true
		if !utf8.ValidString(entry.Preview) || utf8.RuneCountInString(entry.Preview) > 161 || !strings.HasSuffix(entry.Preview, "…") || strings.ContainsAny(entry.Preview, "\n\t") {
			t.Fatalf("unbounded or malformed preview: %q", entry.Preview)
		}

		full, err := memory.Get(ctx, entry.ID)
		if err != nil || full.Content != content || full.Scope != "selected" {
			t.Fatalf("canonical entry changed: %+v, %v", full, err)
		}
	}

	last, err := listCatalog(ctx, memory, "selected", page.NextID)
	if err != nil {
		t.Fatal(err)
	}

	if len(last.Entries) != 1 || last.NextID != "" || seen[last.Entries[0].ID] {
		t.Fatalf("last page = %+v", last)
	}
}
