package discovery

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestDiscoveryToolsReturnSummariesAndLoadKnownNames(t *testing.T) {
	remote := kittest.NewTool(t, "remote", "Read records\nLong instructions")

	set := registeredSet(t, []kit.Tool{remote}, remote)
	for _, tool := range set.Tools() {
		arguments := map[string]any{"query": "read records"}
		if tool.Definition().Name == LoadToolName {
			arguments = map[string]any{"names": []any{"remote"}}
		}

		output, err := tool.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", tool.Definition().Name, arguments))
		if err != nil {
			t.Fatal(err)
		}

		data, err := json.Marshal(output)
		if err != nil || !strings.Contains(string(data), "remote") || strings.Contains(string(data), "Long instructions") || strings.Contains(string(data), "schema") {
			t.Fatalf("discovery output = %s, %v", data, err)
		}
	}

	remote.AssertNeverCalled(t)
}
