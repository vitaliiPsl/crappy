package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/internal/api"
	"github.com/vitaliipsl/crappy/internal/daemon"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/schedule"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/tools"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestServeExposesAssistantOverSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, client := newServeFixture(ctx, t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg-test\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
	}))

	if sessions, err := client.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("daemon files appeared as conversations: %+v, %v", sessions, err)
	}

	session, err := client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Context(ctx, session.ID)
	if err != nil {
		t.Fatalf("context tools=%d, %v", len(report.Tools), err)
	}

	for _, name := range []string{"schedule_create", "schedule_list", "schedule_cancel"} {
		found := false
		for _, tool := range report.Tools {
			if tool.Name == name {
				found = true
			}
		}

		if !found {
			t.Fatalf("missing tool %s", name)
		}
	}

	id, err := client.Execute(ctx, session.ID, kit.NewTextContent("Say done"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("outcome=%s, %v", status, err)
	}

	waitForModelReply(ctx, t, client, session.ID, "done")
}

func TestServeRunsScheduledWorkWithoutConnectedClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	outputs := []string{
		`{"type":"function_call","id":"fc-schedule","call_id":"call-schedule","name":"schedule_create","arguments":"{\"instruction\":\"Write scheduled-complete into result.txt\",\"at\":\"%s\"}"}`,
		`{"type":"message","id":"msg-scheduled","role":"assistant","content":[{"type":"output_text","text":"scheduled"}]}`,
		`{"type":"function_call","id":"fc-bash","call_id":"call-bash","name":"bash","arguments":"{\"command\":\"printf scheduled-complete > result.txt\",\"description\":\"Write the scheduled result\"}"}`,
		`{"type":"message","id":"msg-result","role":"assistant","content":[{"type":"output_text","text":"scheduled-complete"}]}`,
	}

	var calls atomic.Int32

	root, client := newServeFixture(ctx, t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(outputs) {
			t.Errorf("unexpected model call %d", index)
			http.Error(w, "unexpected model call", http.StatusInternalServerError)

			return
		}

		output := outputs[index]
		if index == 0 {
			output = fmt.Sprintf(output, time.Now().Add(time.Second).Format(time.RFC3339Nano))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"status\":\"completed\",\"output\":[%s]}}\n\n", output)
	}))

	session, err := client.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	id, err := client.Execute(ctx, session.ID, kit.NewTextContent("Write scheduled-complete into result.txt in one second"))
	if err != nil {
		t.Fatal(err)
	}

	if status, err := client.WaitExecution(ctx, session.ID, id); err != nil || status != execution.Completed {
		t.Fatalf("scheduling execution = %s, %v", status, err)
	}

	records, err := client.ListSchedules(ctx, session.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("scheduled records = %+v, %v", records, err)
	}

	client.Close()

	// No client is connected while the daemon carries out the scheduled work.
	for {
		result, err := os.ReadFile(filepath.Join(root, "result.txt"))
		if err == nil && string(result) == "scheduled-complete" {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatalf("scheduled result missing: %s, %v; model calls = %d", result, err, calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	reconnected := api.NewClient(filepath.Join(root, "daemon", "socket"), root)
	defer reconnected.Close()

	scheduledID := waitForModelReply(ctx, t, reconnected, session.ID, "scheduled-complete")
	if status, err := reconnected.WaitExecution(ctx, session.ID, scheduledID); err != nil || status != execution.Completed {
		t.Fatalf("scheduled execution = %s, %v", status, err)
	}

	record, err := reconnected.GetSchedule(ctx, session.ID, records[0].ID)
	if err != nil || record.State != schedule.Dispatched {
		t.Fatalf("schedule after reconnect = %+v, %v", record, err)
	}

	if calls.Load() != 4 {
		t.Fatalf("model calls = %d", calls.Load())
	}
}

func TestDaemonDirectoryFollowsSettingsFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if got := daemonDirectory(filepath.Join(root, "settings.yaml")); got != filepath.Join(root, "daemon") {
		t.Fatalf("daemon directory = %q", got)
	}
}

func TestServeRejectsSharedSessionStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	root, client := newServeFixture(ctx, t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("ownership check called the model")
	}))
	otherDirectory := filepath.Join(root, "other-daemon")

	configuration := settings.Settings{SessionsPath: filepath.Join(root, "sessions")}
	if err := serve(ctx, configuration, otherDirectory); !errors.Is(err, daemon.ErrRunning) {
		t.Fatalf("second daemon acquired the same session storage: %v", err)
	}

	instance, err := daemon.Acquire(otherDirectory)
	if err != nil {
		t.Fatalf("failed startup leaked daemon ownership: %v", err)
	}

	defer func() { _ = instance.Close() }()

	if _, err := client.Health(ctx); err != nil {
		t.Fatalf("existing daemon affected by second startup: %v", err)
	}
}

func newServeFixture(ctx context.Context, t *testing.T, handler http.Handler) (string, *api.Client) {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "crappy-cmd-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("provider_id: test\nmodel_id: test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	settings := settings.Settings{
		ConfigPath:   configPath,
		SessionsPath: filepath.Join(root, "sessions"),
		Scheduling:   schedule.DefaultConfig(),
		Memory:       memory.Config{Path: filepath.Join(root, "memory.json")},
		Tools:        tools.DefaultConfig(),
		Providers:    []model.Provider{{ID: "test", API: "openai", APIKey: "test", BaseURL: server.URL}},
	}
	directory := daemonDirectory(filepath.Join(root, "settings.yaml"))
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
	t.Cleanup(client.Close)

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

	return root, client
}

func waitForModelReply(ctx context.Context, t *testing.T, client *api.Client, sessionID, reply string) string {
	t.Helper()

	subscription, err := client.Subscribe(ctx, sessionID, 0)
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

			if message.Role == kit.RoleModel && kit.ContentsText(message.Content) == reply {
				return value.ExecutionID
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
