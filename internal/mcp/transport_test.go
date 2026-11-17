package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPTransportSendsConfiguredHeaders(t *testing.T) {
	t.Setenv("CRAPPY_MCP_TEST_TOKEN", "secret")

	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return newTestServer() }, nil)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Workspace") != "work" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	transport := NewTransport(ServerConfig{URL: httpServer.URL, Headers: map[string]string{
		"Authorization": "Bearer ${CRAPPY_MCP_TEST_TOKEN}", "X-Workspace": "work",
	}})

	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatalf("connection without configured headers: %v", err)
	}

	_ = session.Close()
}

func TestStdioTransportInheritsAndOverridesEnvironment(t *testing.T) {
	t.Setenv("CRAPPY_MCP_TEST_ENV", "inherited")
	t.Setenv("CRAPPY_MCP_TEST_KEPT", "kept")

	transport, ok := NewTransport(ServerConfig{
		Command: "server", Args: []string{"--stdio"}, Env: map[string]string{"CRAPPY_MCP_TEST_ENV": "overridden"},
	}).(*mcpsdk.CommandTransport)
	if !ok {
		t.Fatal("command server did not use a stdio transport")
	}

	command := transport.Command
	if !slices.Contains(command.Env, "CRAPPY_MCP_TEST_ENV=overridden") || !slices.Contains(command.Env, "CRAPPY_MCP_TEST_KEPT=kept") || command.Args[1] != "--stdio" {
		t.Fatalf("stdio command lost configuration: %+v", command)
	}
}

func TestHeaderTransportPreservesRequestHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "caller" || r.Header.Get("X-Workspace") != "work" {
			t.Error("header transport overwrote the caller's header or omitted a configured one")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "caller")

	response, err := NewHeaderTransport(http.Header{"Authorization": {"configured"}, "X-Workspace": {"work"}}).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if request.Header.Get("X-Workspace") != "" {
		t.Fatal("header transport mutated its input request")
	}
}
