package discovery

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
	"github.com/vitaliiPsl/crappy-adk/x/toolset"

	"github.com/vitaliipsl/crappy/harness/token"
)

func TestDiscoveryKeepsLargeCatalogOutOfModelContext(t *testing.T) {
	var external []kit.Tool
	for index := range 50 {
		external = append(external, kittest.NewTool(t, fmt.Sprintf("remote_%02d", index), strings.Repeat("Tool documentation. ", 100)))
	}

	set := registeredSet(t, external, external...)
	all := append(slices.Clone(external), NewTools(set)...)
	estimator := token.NewCharacterEstimator()
	full := token.EstimateRequest(estimator, kit.ModelRequest{Tools: all})

	initial := token.EstimateRequest(estimator, kit.ModelRequest{Tools: set.Tools()})
	if initial*10 >= full {
		t.Fatalf("catalog leaked into initial request: initial=%d full=%d", initial, full)
	}

	if _, err := set.Load("remote_00"); err != nil {
		t.Fatal(err)
	}

	loaded := token.EstimateRequest(estimator, kit.ModelRequest{Tools: set.Tools()})
	if loaded <= initial || loaded*10 >= full {
		t.Fatalf("loading did not isolate one definition: initial=%d loaded=%d full=%d", initial, loaded, full)
	}

	t.Logf("estimated tool tokens: full=%d initial=%d one loaded=%d", full, initial, loaded)
}

func TestSearchAndExactLoadSelectWithoutExecuting(t *testing.T) {
	issues := kittest.NewTool(t, "mcp__linear__list_issues", "Find issues by assignee")
	projects := kittest.NewTool(t, "mcp__linear__list_projects", "List projects")
	core := kittest.NewTool(t, "read_file", "Read a file")

	set := registeredSet(t, []kit.Tool{issues, projects}, core, issues, projects)
	if tools := set.Tools(); len(tools) != 3 || tools[2] != core {
		t.Fatalf("initial tools = %v", tools)
	}

	matches, err := set.Search("Linear issue assignee", 1)
	if err != nil || len(matches) != 1 || matches[0].Name != issues.Definition().Name {
		t.Fatalf("search = %v, %v", matches, err)
	}

	if tools := set.Tools(); len(tools) != 4 || tools[3] != issues {
		t.Fatalf("selected tools = %v", tools)
	}

	if _, err := set.Load(projects.Definition().Name, projects.Definition().Name); err != nil {
		t.Fatal(err)
	}

	if len(set.Tools()) != 5 {
		t.Fatal("exact loading did not expose the tool once")
	}

	issues.AssertNeverCalled(t)
	projects.AssertNeverCalled(t)
	core.AssertNeverCalled(t)
}

func TestFailedLoadDoesNotPartiallySelect(t *testing.T) {
	remote := kittest.NewTool(t, "remote", "")

	set := registeredSet(t, []kit.Tool{remote}, remote)
	if _, err := set.Load("remote", "missing"); err == nil {
		t.Fatal("unknown name loaded")
	}

	if len(set.Tools()) != 2 {
		t.Fatal("failed loading partially changed exposure")
	}

	if _, err := set.Search("!!!", 1); err == nil {
		t.Fatal("empty query accepted")
	}

	if _, err := set.Search("remote", MaxLimit+1); err == nil {
		t.Fatal("unbounded search accepted")
	}

	matches, err := set.Search("unrelated", 1)
	if err != nil || len(matches) != 0 || len(set.Tools()) != 2 {
		t.Fatalf("unrelated search = %v, %v", matches, err)
	}
}

func registeredSet(t *testing.T, deferred []kit.Tool, tools ...kit.Tool) *ToolSet {
	t.Helper()

	set, err := NewToolSet(toolset.New(), func(tool kit.Tool) bool {
		return slices.ContainsFunc(deferred, func(candidate kit.Tool) bool {
			return candidate.Definition().Name == tool.Definition().Name
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := set.Register(tools...); err != nil {
		t.Fatal(err)
	}

	return set
}
