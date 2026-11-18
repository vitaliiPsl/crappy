package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerConnectsInBackgroundAndClosesOnShutdown(t *testing.T) {
	t.Parallel()

	manager := NewManager(Config{ConnectTimeout: 5 * time.Second, Servers: []ServerConfig{{Name: "work", URL: serveHTTP(t, newTestServer()).URL}}}, &testAuthorizer{})
	worker := NewWorker(manager, time.Hour)
	worker.Start()

	for deadline := time.Now().Add(5 * time.Second); manager.Servers()[0].Error != nil; {
		if time.Now().After(deadline) {
			t.Fatalf("worker did not connect: %v", manager.Servers()[0].Error)
		}

		time.Sleep(time.Millisecond)
	}

	echo := findTool(t, manager.Servers()[0].Tools, "mcp__work__echo")

	if err := worker.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if server := manager.Servers()[0]; !errors.Is(server.Error, ErrNotConnected) {
		t.Fatalf("server after shutdown = %+v", server)
	}

	if _, err := callTool(t, echo); err == nil {
		t.Fatal("worker shutdown left the session open")
	}
}

func TestWorkerShutdownInterruptsConnection(t *testing.T) {
	t.Parallel()

	endpoint, requested := serveBlocking(t)
	worker := NewWorker(NewManager(Config{ConnectTimeout: time.Hour, Servers: []ServerConfig{{Name: "slow", URL: endpoint.URL}}}, &testAuthorizer{}), time.Hour)
	worker.Start()

	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start connecting")
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := worker.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown during connection = %v", err)
	}
}
