package discovery

import (
	"slices"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestRankingUsesNamesDescriptionsAndParameterMetadata(t *testing.T) {
	entries := []entry{
		newEntry(kit.ToolDefinition{Name: "lookup", Description: "Find a record", Schema: map[string]any{
			"properties": map[string]any{"dueDate": map[string]any{
				"type": "string", "description": "Deadline of the issue",
			}},
		}}),
		newEntry(kit.ToolDefinition{Name: "listIssues", Description: "List issues"}),
	}
	if names := rank(entries, tokenize("deadline due date"), 1); !slices.Equal(names, []string{"lookup"}) {
		t.Fatalf("parameter search = %v", names)
	}

	if names := rank(entries, tokenize("list issue"), 1); !slices.Equal(names, []string{"listIssues"}) {
		t.Fatalf("name search = %v", names)
	}

	if text := summarizeDescription(strings.Repeat("界", 300) + "\nMore documentation"); len([]rune(text)) != 201 {
		t.Fatalf("summary is not bounded: %q", text)
	}
}

func TestRankingFindsNestedSchemaTerms(t *testing.T) {
	nested := map[string]any{
		"properties": map[string]any{
			"filters": map[string]any{
				"type": "array",
				"items": map[string]any{
					"properties": map[string]any{
						"dueDate": map[string]any{"type": "string", "description": "Deadline"},
					},
				},
			},
		},
	}
	for _, variant := range []string{"anyOf", "oneOf", "allOf"} {
		t.Run(variant, func(t *testing.T) {
			entries := []entry{
				newEntry(kit.ToolDefinition{Name: "unrelated"}),
				newEntry(kit.ToolDefinition{Name: "lookup", Schema: map[string]any{variant: []any{nested}}}),
			}
			if names := rank(entries, tokenize("deadline due date"), 1); !slices.Equal(names, []string{"lookup"}) {
				t.Fatalf("nested parameter search = %v", names)
			}
		})
	}
}

func TestRankingPreservesFieldWeightsAndStableTies(t *testing.T) {
	entries := []entry{
		newEntry(kit.ToolDefinition{Name: "description_match", Description: "records records records"}),
		newEntry(kit.ToolDefinition{Name: "records", Description: "records", Schema: map[string]any{"description": "records"}}),
		newEntry(kit.ToolDefinition{Name: "schema_match", Schema: map[string]any{"description": "records"}}),
		newEntry(kit.ToolDefinition{Name: "another_description", Description: "records"}),
	}

	want := []string{"records", "description_match", "another_description", "schema_match"}
	if names := rank(entries, tokenize("records"), len(entries)); !slices.Equal(names, want) {
		t.Fatalf("weighted search = %v, want %v", names, want)
	}
}
