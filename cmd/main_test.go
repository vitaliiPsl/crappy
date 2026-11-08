package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/memory"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
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

	entry, err := memory.New(store).Create(ctx, "personal", "Prefer concise explanations")
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
		SessionsPath: t.TempDir(), MemoryPath: path, Tools: tools.DefaultConfig(),
		Providers: []model.Provider{{ID: "test", API: "openai", APIKey: "test-key", BaseURL: server.URL}},
	}
	config := config.Config{ProviderID: "test", ModelID: "test-model"}

	for range 2 {
		h, app, err := newAssistant(ctx, settings, config, token.NewCharacterEstimator())
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

		if memoryTools != 5 {
			t.Fatalf("memory tools = %d, want 5", memoryTools)
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
