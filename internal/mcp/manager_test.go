package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestManagerConnectsServersIndependently(t *testing.T) {
	t.Parallel()

	manager := NewManager(Config{ConnectTimeout: 5 * time.Second, Servers: []ServerConfig{
		{Name: "healthy", URL: serveHTTP(t, newTestServer()).URL},
		{Name: "broken", Command: "/does-not-exist/crappy-mcp-server"},
	}}, &testAuthorizer{})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	for _, server := range manager.Servers() {
		if !errors.Is(server.Error, ErrNotConnected) {
			t.Fatalf("server before refresh = %+v", server)
		}
	}

	if err := manager.Refresh(t.Context()); err == nil || !strings.Contains(err.Error(), "broken") || strings.Contains(err.Error(), "healthy") {
		t.Fatalf("first refresh = %v", err)
	}

	if err := manager.Refresh(t.Context()); err != nil {
		t.Fatalf("unchanged failure was reported again: %v", err)
	}

	servers := manager.Servers()
	if servers[0].Error != nil || len(servers[0].Tools) != 3 || servers[0].Instructions != "Use these tools for testing." || servers[1].Error == nil {
		t.Fatalf("servers = %+v", servers)
	}

	echo := findTool(t, servers[0].Tools, "mcp__healthy__echo")
	if output, err := callTool(t, echo); err != nil || kit.ContentsText(output.Content) != "hello" {
		t.Fatalf("healthy tool = %+v, %v", output, err)
	}

	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if server := manager.Servers()[0]; !errors.Is(server.Error, ErrNotConnected) || len(server.Tools) != 0 {
		t.Fatalf("server after shutdown = %+v", server)
	}

	if _, err := callTool(t, echo); err == nil {
		t.Fatal("closed session still served a call")
	}
}

func TestManagerReconnectsDroppedServer(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	manager := connected(t, ServerConfig{
		Name: "stdio", Command: executable, Args: []string{"-test.run=^TestMCPStdioServer$"},
		Env: map[string]string{"CRAPPY_MCP_TEST_SERVER": "1"},
	})

	dropped := manager.Servers()[0]
	if dropped.Error != nil || len(dropped.Tools) != 4 {
		t.Fatalf("stdio startup = %+v", dropped)
	}

	_, _ = callTool(t, findTool(t, dropped.Tools, "mcp__stdio__disconnect"))

	stale := findTool(t, dropped.Tools, "mcp__stdio__echo")
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := callTool(t, stale); err != nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("server did not disconnect")
		}

		time.Sleep(time.Millisecond)
	}

	if err := manager.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	echo := findTool(t, manager.Servers()[0].Tools, "mcp__stdio__echo")
	if output, err := callTool(t, echo); err != nil || kit.ContentsText(output.Content) != "hello" {
		t.Fatalf("reconnected tool = %+v, %v", output, err)
	}
}

func TestManagerConnectsThroughAuthorizer(t *testing.T) {
	t.Parallel()

	authorizer := &testAuthorizer{}
	manager := NewManager(Config{ConnectTimeout: 5 * time.Second, Servers: []ServerConfig{{Name: "work", URL: serveProtected(t).URL}}}, authorizer)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if err := manager.Refresh(t.Context()); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("refresh before login = %v", err)
	}

	authorizer.token.Store(&oauth2.Token{AccessToken: "access"})

	if err := manager.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	if server := manager.Servers()[0]; server.Error != nil || len(server.Tools) != 3 {
		t.Fatalf("authorized server = %+v", server)
	}
}

func TestManagerBoundsConnection(t *testing.T) {
	t.Parallel()

	endpoint, _ := serveBlocking(t)
	manager := NewManager(Config{ConnectTimeout: 100 * time.Millisecond, Servers: []ServerConfig{{Name: "slow", URL: endpoint.URL}}}, &testAuthorizer{})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	started := time.Now()

	if err := manager.Refresh(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow server = %v, want deadline exceeded", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("connection deadline took %s", elapsed)
	}
}

func TestMCPStdioServer(_ *testing.T) {
	if os.Getenv("CRAPPY_MCP_TEST_SERVER") != "1" {
		return
	}

	server := newTestServer()
	server.AddTool(&mcpsdk.Tool{Name: "disconnect", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			go func() { _ = req.Session.Close() }()

			return &mcpsdk.CallToolResult{}, nil
		})

	_ = server.Run(context.Background(), &mcpsdk.StdioTransport{})

	os.Exit(0)
}

func newTestServer() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"},
		&mcpsdk.ServerOptions{Instructions: "Use these tools for testing.", PageSize: 1})
	for _, name := range []string{"echo", "large", "slow"} {
		server.AddTool(&mcpsdk.Tool{
			Name: name, Description: "A test tool",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			if req.Params.Name == "slow" {
				<-ctx.Done()

				return nil, ctx.Err()
			}

			text := "hello"
			if req.Params.Name == "large" {
				text = strings.Repeat("large output\n", 1000)
			}

			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}, nil
		})
	}

	return server
}

func serveHTTP(t *testing.T, server *mcpsdk.Server) *httptest.Server {
	t.Helper()

	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil))
	t.Cleanup(httpServer.Close)

	return httpServer
}

func serveProtected(t *testing.T) *httptest.Server {
	t.Helper()

	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return newTestServer() }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)

	return httpServer
}

func serveBlocking(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()

	requested := make(chan struct{}, 1)
	release := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SDK waits up to 5s for each cancellation notice, so a server ignoring them outlasts any connect timeout.
		if body, _ := io.ReadAll(r.Body); strings.Contains(string(body), "notifications/cancelled") {
			w.WriteHeader(http.StatusAccepted)

			return
		}

		select {
		case requested <- struct{}{}:
		default:
		}

		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		httpServer.Close()
	})

	return httpServer, requested
}

func connected(t *testing.T, servers ...ServerConfig) *Manager {
	t.Helper()

	manager := NewManager(Config{ConnectTimeout: 5 * time.Second, Servers: servers}, &testAuthorizer{})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if err := manager.Refresh(t.Context()); err != nil {
		t.Logf("refresh: %v", err)
	}

	return manager
}

type testAuthorizer struct {
	token atomic.Pointer[oauth2.Token]
}

func (a *testAuthorizer) Handler(ServerConfig) (auth.OAuthHandler, error) {
	return a, nil
}

func (a *testAuthorizer) TokenSource(context.Context) (oauth2.TokenSource, error) {
	token := a.token.Load()
	if token == nil {
		return nil, nil
	}

	return oauth2.StaticTokenSource(token), nil
}

func (a *testAuthorizer) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	_ = response.Body.Close()

	return ErrAuthorizationRequired
}

func findTool(t *testing.T, tools []kit.Tool, name string) kit.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool
		}
	}

	t.Fatalf("tool %s not found", name)

	return nil
}

func callTool(t *testing.T, tool kit.Tool) (kit.ToolOutput, error) {
	t.Helper()

	return tool.Execute(kit.NewRunContext(t.Context()), kit.NewToolCall("call", tool.Definition().Name, map[string]any{"text": "hello"}))
}
