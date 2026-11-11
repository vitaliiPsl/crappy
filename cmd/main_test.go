package main

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

	"github.com/vitaliiPsl/crappy-adk/kit"

	corememory "github.com/vitaliipsl/crappy-memory/memory"
	jsonstore "github.com/vitaliipsl/crappy-memory/storage/json"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/tools"
)

func TestNewAssistantLoadsMemoryAcrossFreshSessionsAndRestarts(t *testing.T) {
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
		SessionsPath: t.TempDir(),
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
	config := config.Config{ProviderID: "test", ModelID: "test-model"}

	for range 2 {
		memories, err := memory.Open(ctx, settings.Memory)
		if err != nil {
			t.Fatal(err)
		}

		h, app, err := newAssistant(ctx, settings, config, token.NewCharacterEstimator(), memories)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = h.Shutdown(ctx) })

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

		if err := h.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("context inspection changed memory: %s, %v", after, err)
	}
}

func TestNewAssistantSearchesSavedMemoriesAfterRestart(t *testing.T) {
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
		SessionsPath: t.TempDir(),
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

	config := config.Config{
		ProviderID: "test",
		ModelID:    "test-model",
		Compaction: config.Compaction{ThresholdTokens: 1_000_000},
	}
	for range 2 {
		memories, err := memory.Open(ctx, settings.Memory)
		if err != nil {
			t.Fatal(err)
		}

		h, app, err := newAssistant(ctx, settings, config, token.NewCharacterEstimator(), memories)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

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

		if err := h.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if requests.Load() != 4 || searches.Load() != 2 {
		t.Fatalf("model requests=%d, memory searches=%d; want 4 and 2", requests.Load(), searches.Load())
	}
}
