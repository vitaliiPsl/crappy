package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/vitaliipsl/crappy/internal/mcp"
)

func TestAuthorizerUsesStoredCredential(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	config := mcp.ServerConfig{Name: "work", URL: "https://example.com/mcp"}

	if err := store.Save("work", Credential{URL: config.URL, Token: oauth2.Token{AccessToken: "access"}}); err != nil {
		t.Fatal(err)
	}

	handler, err := NewAuthorizer(store).Handler(config)
	if err != nil {
		t.Fatal(err)
	}

	tokens, err := handler.TokenSource(t.Context())
	if err != nil || tokens == nil {
		t.Fatalf("token source = %v, %v", tokens, err)
	}

	if token, err := tokens.Token(); err != nil || token.AccessToken != "access" {
		t.Fatalf("stored token = %+v, %v", token, err)
	}
}

func TestAuthorizerIgnoresCredentialForAnotherURL(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	if err := store.Save("work", Credential{URL: "https://old.example.com/mcp", Token: oauth2.Token{AccessToken: "access"}}); err != nil {
		t.Fatal(err)
	}

	handler, err := NewAuthorizer(store).Handler(mcp.ServerConfig{Name: "work", URL: "https://new.example.com/mcp"})
	if err != nil {
		t.Fatal(err)
	}

	if tokens, err := handler.TokenSource(t.Context()); tokens != nil || err != nil {
		t.Fatalf("token source for another URL = %v, %v", tokens, err)
	}
}

func TestAuthorizerWithCallbackLogsInThroughConnection(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	config := mcp.ServerConfig{Name: "work", URL: serveAuthorization(t).URL + "/mcp"}

	servers := connect(t, config, NewAuthorizer(store, WithCallback(newCallback(t, func(url string) { visit(t, url) }))))
	if servers[0].Error != nil || len(servers[0].Tools) != 1 {
		t.Fatalf("server after login = %+v", servers[0])
	}

	credential, err := store.Load("work")
	if err != nil || credential == nil || credential.URL != config.URL || credential.Config.ClientID != "crappy-client" ||
		credential.Token.AccessToken != "access" || credential.Token.RefreshToken != "refresh" {
		t.Fatalf("stored credential = %+v, %v", credential, err)
	}
}

func TestAuthorizerWithCallbackReusesStoredCredential(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	config := mcp.ServerConfig{Name: "work", URL: serveAuthorization(t).URL + "/mcp"}

	if err := store.Save("work", Credential{URL: config.URL, Token: oauth2.Token{AccessToken: "access"}}); err != nil {
		t.Fatal(err)
	}

	callback := newCallback(t, func(string) { t.Error("opened the browser despite a valid stored credential") })
	if servers := connect(t, config, NewAuthorizer(store, WithCallback(callback))); servers[0].Error != nil {
		t.Fatalf("server with stored credential = %+v", servers[0])
	}
}

func TestHandlerRequiresLogin(t *testing.T) {
	t.Parallel()

	response := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}
	if err := NewHandler(nil).Authorize(t.Context(), nil, response); !errors.Is(err, mcp.ErrAuthorizationRequired) {
		t.Fatalf("authorize = %v, want authorization required", err)
	}
}

func connect(t *testing.T, config mcp.ServerConfig, authorizer *Authorizer) []mcp.Server {
	t.Helper()

	manager := mcp.NewManager(mcp.Config{ConnectTimeout: 10 * time.Second, Servers: []mcp.ServerConfig{config}}, authorizer)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if err := manager.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	return manager.Servers()
}

func newCallback(t *testing.T, open func(url string)) *Callback {
	t.Helper()

	callback, err := NewCallback(open)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = callback.Close() })

	return callback
}

func serveAuthorization(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resource := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return newServer() }, nil)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("Authorization"); token != "Bearer access" && token != "Bearer refreshed" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+server.URL+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		resource.ServeHTTP(w, r)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"resource": server.URL + "/mcp", "authorization_servers": []string{server.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                           server.URL,
			"authorization_endpoint":           server.URL + "/authorize",
			"token_endpoint":                   server.URL + "/token",
			"registration_endpoint":            server.URL + "/register",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"client_id": "crappy-client"})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		http.Redirect(w, r, query.Get("redirect_uri")+"?code=code&state="+url.QueryEscape(query.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		access, refresh := "access", "refresh"
		if r.FormValue("grant_type") == "refresh_token" {
			access, refresh = "refreshed", "rotated"
		}

		writeJSON(w, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600})
	})

	return server
}

func newServer() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "search", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{}, nil
		})

	return server
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
