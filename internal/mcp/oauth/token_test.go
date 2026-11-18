package oauth

import (
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestTokenSourceRefreshesAndStoresTokens(t *testing.T) {
	t.Parallel()

	server := serveAuthorization(t)
	store := NewStore(t.TempDir())

	tokens := NewTokenSource(store, "work", Credential{
		URL:    server.URL + "/mcp",
		Config: oauth2.Config{ClientID: "crappy-client", Endpoint: oauth2.Endpoint{TokenURL: server.URL + "/token"}},
		Token:  oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)},
	})

	if token, err := tokens.Token(); err != nil || token.AccessToken != "refreshed" {
		t.Fatalf("refreshed token = %+v, %v", token, err)
	}

	if stored, err := store.Load("work"); err != nil || stored.Token.AccessToken != "refreshed" || stored.Token.RefreshToken != "rotated" {
		t.Fatalf("stored credential = %+v, %v", stored, err)
	}
}
