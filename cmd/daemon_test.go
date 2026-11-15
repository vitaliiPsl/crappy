package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/api"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestServeExposesAssistantOverSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	root, err := os.MkdirTemp("/tmp", "crappy-cmd-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg-test\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
	}))
	defer server.Close()

	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("provider_id: test\nmodel_id: test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	settings := settings.Settings{
		ConfigPath:   configPath,
		SessionsPath: filepath.Join(root, "sessions"),
		Memory:       memory.Config{Path: filepath.Join(root, "memory.json")},
		Tools:        tools.DefaultConfig(),
		Providers:    []model.Provider{{ID: "test", API: "openai", APIKey: "test", BaseURL: server.URL}},
	}
	directory := filepath.Join(root, "runtime")
	serverContext, stop := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- serve(serverContext, settings, directory) }()

	t.Cleanup(func() {
		stop()

		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	client := api.NewClient(filepath.Join(directory, "socket"), root)
	defer client.Close()

	for {
		probe, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_, err := client.Health(probe)

		cancel()

		if err == nil {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	session, err := client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Context(ctx, session.ID)
	if err != nil || len(report.Tools) != 10 {
		t.Fatalf("context tools=%d, %v", len(report.Tools), err)
	}

	id, err := client.Execute(ctx, session.ID, kit.NewTextContent("Say done"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("outcome=%s, %v", status, err)
	}

	subscription, err := client.Subscribe(ctx, session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	for {
		select {
		case value, ok := <-subscription.Events():
			if !ok {
				t.Fatalf("stream ended: %v", subscription.Err())
			}

			if value.Type != agent.MessageEntryType {
				continue
			}

			var message kit.Message
			if err := json.Unmarshal(value.Payload, &message); err != nil {
				t.Fatal(err)
			}

			if message.Role == kit.RoleModel {
				if text := kit.ContentsText(message.Content); text != "done" {
					t.Fatalf("reply=%q", text)
				}

				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
