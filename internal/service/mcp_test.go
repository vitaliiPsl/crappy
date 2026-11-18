package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
)

func TestListMCPServersReportsConnectionStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	storage, err := jsonl.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	h := harness.New(storage)
	t.Cleanup(func() { _ = h.Shutdown(ctx) })

	tools := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	tools.AddTool(&mcpsdk.Tool{Name: "search", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{}, nil
		})

	open := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return tools }, nil))
	t.Cleanup(open.Close)

	locked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(locked.Close)

	servers := mcp.NewManager(mcp.Config{ConnectTimeout: 5 * time.Second, Servers: []mcp.ServerConfig{
		{Name: "work", URL: open.URL},
		{Name: "locked", URL: locked.URL},
		{Name: "broken", Command: "/does-not-exist/crappy-mcp-server"},
	}}, loginRequired{})
	t.Cleanup(func() { _ = servers.Shutdown(context.Background()) })

	local := NewLocal(config.Config{}, h, storage, nil, token.NewCharacterEstimator(), servers, nil)

	before, err := local.ListMCPServers(ctx)
	if err != nil || len(before) != 3 || before[0] != (MCPServer{Name: "work", Status: MCPServerConnecting}) {
		t.Fatalf("servers before connecting = %+v, %v", before, err)
	}

	_ = servers.Refresh(ctx)

	after, err := local.ListMCPServers(ctx)
	if err != nil || len(after) != 3 {
		t.Fatalf("servers after connecting = %+v, %v", after, err)
	}

	if after[0] != (MCPServer{Name: "work", Status: MCPServerConnected, Tools: 1}) ||
		after[1] != (MCPServer{Name: "locked", Status: MCPServerAuthorizationRequired}) ||
		after[2].Name != "broken" || after[2].Status != MCPServerUnavailable || after[2].Error == "" {
		t.Fatalf("servers after connecting = %+v", after)
	}
}

type loginRequired struct{}

func (loginRequired) Handler(mcp.ServerConfig) (auth.OAuthHandler, error) {
	return loginRequired{}, nil
}

func (loginRequired) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return nil, nil
}

func (loginRequired) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	_ = response.Body.Close()

	return mcp.ErrAuthorizationRequired
}
