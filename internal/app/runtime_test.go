package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	corememory "github.com/vitaliipsl/crappy-memory/memory"
	jsonstore "github.com/vitaliipsl/crappy-memory/storage/json"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/interaction"
	"github.com/vitaliipsl/crappy/internal/background"
	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/prompt"
	"github.com/vitaliipsl/crappy/internal/schedule"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestRuntimeUsesMCPToolsAndClosesConnections(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var toolCalls, modelCalls atomic.Int32

	remote := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "records", Version: "1"},
		&mcpsdk.ServerOptions{Instructions: "Use search to find work records."})
	remote.AddTool(&mcpsdk.Tool{
		Name: "search", Description: "Find work records",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
	}, func(_ context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var arguments map[string]any
		if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil || len(arguments) != 1 || arguments["query"] != "latest" {
			t.Errorf("remote tool arguments = %+v, %v", arguments, err)
		}

		toolCalls.Add(1)

		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "The latest record is ready."}}}, nil
	})

	endpoint := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return remote }, nil))
	defer endpoint.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []struct {
				Type   string          `json:"type"`
				Output json.RawMessage `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		output := "[]"
		if modelCalls.Add(1) == 1 {
			output = `[{"type":"function_call","id":"search","call_id":"search","name":"mcp__work__search","arguments":"{\"query\":\"latest\",\"timeout_seconds\":1}"}]`
		} else {
			found := false
			for _, item := range body.Input {
				if item.Type == "function_call_output" && strings.Contains(string(item.Output), "latest record is ready") {
					found = true
				}
			}

			if !found {
				t.Error("model did not receive the MCP tool result")
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":%s}}\n\n", output)
	}))
	defer modelServer.Close()

	config := mcp.DefaultConfig()
	config.Servers = []mcp.ServerConfig{{Name: "work", URL: endpoint.URL}}

	runtime, err := Open(ctx, settings.Settings{
		ConfigPath: runtimeConfig(t), SessionsPath: t.TempDir(), Scheduling: schedule.DefaultConfig(),
		Memory: memory.Config{Path: filepath.Join(t.TempDir(), "memory.json")}, Tools: tools.DefaultConfig(), MCP: config,
		Providers: []model.Provider{{ID: "test", API: "openai", APIKey: "test-key", BaseURL: modelServer.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = runtime.Shutdown(context.Background()) }()

	app := runtime.Service()

	opened, err := app.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for found := false; !found; {
		report, err := app.Context(ctx, opened.ID)
		if err != nil {
			t.Fatal(err)
		}

		for _, tool := range report.Tools {
			if tool.Name == "mcp__work__search" {
				found = true

				if _, exists := tool.Schema["properties"].(map[string]any)["timeout_seconds"]; !exists {
					t.Fatal("MCP tool omitted the existing timeout wrapper")
				}
			}
		}

		select {
		case <-ctx.Done():
			t.Fatal("MCP tool was never contributed")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if toolCalls.Load() != 0 || modelCalls.Load() != 0 {
		t.Fatalf("context inspection executed work: tools=%d, model=%d", toolCalls.Load(), modelCalls.Load())
	}

	id, err := app.Execute(ctx, opened.ID, kit.NewTextContent("Find the latest work record"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := app.WaitExecution(ctx, opened.ID, id); err != nil || status != execution.Completed || toolCalls.Load() != 1 || modelCalls.Load() != 2 {
		t.Fatalf("MCP execution = %s, %v; tool calls=%d, model calls=%d", status, err, toolCalls.Load(), modelCalls.Load())
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeWaitsForUserWithoutToolDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input []struct {
				Type   string `json:"type"`
				Output string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)

			return
		}

		output := "[]"
		switch requests.Add(1) {
		case 1:
			output = `[{"type":"function_call","id":"question","call_id":"deadline","name":"ask_user","arguments":"{\"question\":\"Which deadline?\",\"choices\":[{\"id\":\"soon\",\"label\":\"As soon as possible\"}]}"}]`
		case 2:
			found := false
			for _, item := range body.Input {
				if item.Type == "function_call_output" && strings.Contains(item.Output, "Friday") {
					found = true
				}
			}

			if !found {
				t.Error("the agent did not receive the user's answer")
			}
		case 3:
		default:
			t.Error("unexpected model request")
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":%s}}\n\n", output)
	}))
	defer server.Close()

	toolConfig := tools.DefaultConfig()
	toolConfig.Timeout = 20 * time.Millisecond

	runtime, err := Open(ctx, settings.Settings{
		ConfigPath: runtimeConfig(t), SessionsPath: t.TempDir(), Scheduling: schedule.DefaultConfig(),
		Memory: memory.Config{Path: filepath.Join(t.TempDir(), "memory.json")}, Tools: toolConfig, MCP: mcp.DefaultConfig(),
		Providers: []model.Provider{{ID: "test", API: "openai", APIKey: "test-key", BaseURL: server.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })

	app := runtime.Service()

	opened, err := app.CreateSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	report, err := app.Context(ctx, opened.ID)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, tool := range report.Tools {
		if tool.Name == prompt.ToolName {
			found = true

			properties := tool.Schema["properties"].(map[string]any)
			if _, hasDeadline := properties["timeout_seconds"]; hasDeadline {
				t.Fatal("the question tool exposes the ordinary tool deadline")
			}
		}
	}

	if !found || requests.Load() != 0 {
		t.Fatalf("inspection omitted ask_user or called the model: found=%v, calls=%d", found, requests.Load())
	}

	subscription, err := app.Subscribe(ctx, opened.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	id, err := app.Execute(ctx, opened.ID, kit.NewTextContent("Prepare a report"))
	if err != nil {
		t.Fatal(err)
	}

	var question interaction.Request
	for question.ID == "" {
		select {
		case value := <-subscription.Events():
			if value.Type == interaction.RequestedEntryType {
				if err := json.Unmarshal(value.Payload, &question); err != nil {
					t.Fatal(err)
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	subscription.Close()
	<-time.After(3 * toolConfig.Timeout)

	if pending, err := app.PendingPrompts(ctx, opened.ID); err != nil || len(pending) != 1 {
		t.Fatalf("question expired at the tool deadline: %+v, %v", pending, err)
	}

	if err := app.Respond(ctx, opened.ID, question.ID, interaction.Answer{Text: "Friday"}); err != nil {
		t.Fatal(err)
	}

	if status, err := app.WaitExecution(ctx, opened.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("answered execution = %s, %v", status, err)
	}

	id, err = app.Execute(ctx, opened.ID, kit.NewTextContent("A straightforward request"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := app.WaitExecution(ctx, opened.ID, id); err != nil || status != execution.Completed || requests.Load() != 3 {
		t.Fatalf("ordinary execution = %s, %v; model calls=%d", status, err, requests.Load())
	}
}

func TestRuntimeLoadsMemoryAcrossFreshSessionsAndRestarts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(t.TempDir(), "memory.json")

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	entry, err := corememory.New(store).Create(ctx, "personal", "Prefer concise explanations")
	if err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("context inspection called the model")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	settings := settings.Settings{
		ConfigPath:   runtimeConfig(t),
		SessionsPath: t.TempDir(),
		Scheduling:   schedule.DefaultConfig(),
		MCP:          mcp.DefaultConfig(),
		Memory:       memory.Config{Path: path},
		Tools:        tools.DefaultConfig(),
		Providers: []model.Provider{
			{
				ID:      "test",
				API:     "openai",
				APIKey:  "test-key",
				BaseURL: server.URL,
			},
		},
	}

	for range 2 {
		runtime, err := Open(ctx, settings)
		if err != nil {
			t.Fatal(err)
		}

		app := runtime.Service()

		t.Cleanup(func() { _ = runtime.Shutdown(ctx) })

		session, err := app.CreateSession(ctx, directory)
		if err != nil {
			t.Fatal(err)
		}

		report, err := app.Context(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, instruction := range report.Instructions {
			if instruction.Source == "Memory" && strings.Contains(instruction.Text, entry.ID) {
				found = true
			}
		}

		if !found {
			t.Fatalf("fresh session context omitted persistent memory: %+v", report.Instructions)
		}

		memoryTools := 0
		for _, tool := range report.Tools {
			if strings.HasPrefix(tool.Name, "memory_") {
				memoryTools++
			}
		}

		if memoryTools != 6 {
			t.Fatalf("memory tools = %d, want 6", memoryTools)
		}

		if err := runtime.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("context inspection changed memory: %s, %v", after, err)
	}
}

func TestRuntimeSearchesSavedMemoriesAfterRestart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "memory.json")

	store, err := jsonstore.New(path)
	if err != nil {
		t.Fatal(err)
	}

	entry, err := corememory.New(store).Create(ctx, "personal", "Prefer concise explanations")
	if err != nil {
		t.Fatal(err)
	}

	var requests, searches atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if requests.Add(1) > 4 {
			t.Error("unexpected model request")
			writer.WriteHeader(http.StatusInternalServerError)

			return
		}

		var body struct {
			Input []struct {
				Type   string `json:"type"`
				Output string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)

			return
		}

		output := `[{"type":"function_call","id":"search","call_id":"recall","name":"memory_search","arguments":"{\"query\":\"concise\"}"}]`
		for _, item := range body.Input {
			if item.Type != "function_call_output" {
				continue
			}

			var results []corememory.SearchResult
			if err := json.Unmarshal([]byte(item.Output), &results); err != nil {
				t.Errorf("decode search results: %v; output=%s", err, item.Output)
			} else if len(results) != 1 || results[0].Entry.ID != entry.ID || results[0].Entry.Content != entry.Content || results[0].Score <= 0 {
				t.Errorf("search omitted saved memory: %+v", results)
			}

			searches.Add(1)

			output = "[]"

			break
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":%s}}\n\n", output)
	}))
	defer server.Close()

	settings := settings.Settings{
		ConfigPath:   runtimeConfig(t),
		SessionsPath: t.TempDir(),
		Scheduling:   schedule.DefaultConfig(),
		MCP:          mcp.DefaultConfig(),
		Memory:       memory.Config{Path: path},
		Tools:        tools.DefaultConfig(),
		Providers: []model.Provider{
			{
				ID:      "test",
				API:     "openai",
				APIKey:  "test-key",
				BaseURL: server.URL,
			},
		},
	}

	for range 2 {
		runtime, err := Open(ctx, settings)
		if err != nil {
			t.Fatal(err)
		}

		app := runtime.Service()

		t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })

		session, err := app.CreateSession(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		id, err := app.Execute(ctx, session.ID, kit.NewTextContent("Recall my preferences"))
		if err != nil {
			t.Fatal(err)
		}

		if status, err := app.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
			t.Fatalf("memory recall execution = %s, %v", status, err)
		}

		if err := runtime.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if requests.Load() != 4 || searches.Load() != 2 {
		t.Fatalf("model requests=%d, memory searches=%d; want 4 and 2", requests.Load(), searches.Load())
	}
}

func runtimeConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider_id: test\nmodel_id: test-model\ncompaction:\n  threshold_tokens: 1000000\n"), 0600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestRuntimeBackgroundJobContinuesAfterExecutionAndCapsRetrievedOutput(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	directory := t.TempDir()

	var (
		requests atomic.Int32
		jobID    atomic.Value
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input []struct {
				Type   string          `json:"type"`
				CallID string          `json:"call_id"`
				Output json.RawMessage `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)

			return
		}

		output := []any{}
		switch requests.Add(1) {
		case 1:
			arguments, _ := json.Marshal(map[string]any{
				"command":     "while [ ! -f release ]; do sleep 0.01; done; i=0; while [ \"$i\" -lt 3000 ]; do printf 'background-output\\n'; i=$((i+1)); done",
				"description": "Produce a large result after release", "background_timeout_seconds": 60,
			})
			output = []any{map[string]any{
				"type": "function_call", "id": "start", "call_id": "start-job", "name": "bash", "arguments": string(arguments),
			}}
		case 2:
			for _, item := range body.Input {
				if item.Type != "function_call_output" || item.CallID != "start-job" {
					continue
				}

				var receipt string
				if err := json.Unmarshal(item.Output, &receipt); err != nil {
					t.Error(err)

					continue
				}

				var record background.Record
				if err := json.Unmarshal([]byte(receipt), &record); err != nil || record.ID == "" {
					t.Errorf("background admission = %s, %v", item.Output, err)
				}

				jobID.Store(record.ID)
			}
		case 3:
			arguments, _ := json.Marshal(map[string]any{"id": jobID.Load()})
			output = []any{map[string]any{
				"type": "function_call", "id": "get", "call_id": "get-job", "name": "job_get", "arguments": string(arguments),
			}}
		case 4:
			found := false
			for _, item := range body.Input {
				if item.Type == "function_call_output" && item.CallID == "get-job" {
					found = strings.Contains(string(item.Output), "background-output") && strings.Contains(string(item.Output), "full output:") && len(item.Output) < 2000
				}
			}

			if !found {
				t.Error("model did not receive the capped background output")
			}
		default:
			t.Error("unexpected model request")
			writer.WriteHeader(http.StatusInternalServerError)

			return
		}

		data, err := json.Marshal(output)
		if err != nil {
			t.Error(err)

			return
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":%s}}\n\n", data)
	}))
	defer server.Close()

	runtime, err := Open(ctx, settings.Settings{
		ConfigPath:   runtimeConfig(t),
		SessionsPath: t.TempDir(),
		Scheduling:   schedule.DefaultConfig(),
		MCP:          mcp.DefaultConfig(),
		Memory:       memory.Config{Path: filepath.Join(t.TempDir(), "memory.json")},
		Tools: tools.Config{
			Timeout:              time.Second,
			MaxTimeout:           2 * time.Second,
			MaxBackgroundTimeout: time.Minute,
			MaxOutputBytes:       1024,
		},
		Providers: []model.Provider{{
			ID: "test", API: "openai", APIKey: "test-key", BaseURL: server.URL,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })

	app := runtime.Service()

	opened, err := app.CreateSession(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := app.Subscribe(ctx, opened.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	first, err := app.Execute(ctx, opened.ID, kit.NewTextContent("Run work in the background"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := app.WaitExecution(ctx, opened.ID, first); err != nil || status != execution.Completed {
		t.Fatalf("launcher execution = %s, %v", status, err)
	}

	if requests.Load() != 2 || jobID.Load() == nil {
		t.Fatalf("launcher did not return before background work finished: requests=%d, job=%v", requests.Load(), jobID.Load())
	}

	if err := os.WriteFile(filepath.Join(directory, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for {
		select {
		case value := <-subscription.Events():
			if value.Type != execution.EntryType || value.ExecutionID == first {
				continue
			}

			var record execution.Record
			if err := json.Unmarshal(value.Payload, &record); err != nil {
				t.Fatal(err)
			}

			if record.Status.Terminal() {
				if record.Status != execution.Completed || requests.Load() != 4 {
					t.Fatalf("completion execution = %+v, requests=%d", record, requests.Load())
				}

				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
