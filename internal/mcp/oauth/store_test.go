package oauth

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func TestStoreKeepsEachServerPrivate(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "mcp")
	store := NewStore(directory)

	if credential, err := store.Load("work"); credential != nil || err != nil {
		t.Fatalf("missing credential = %+v, %v", credential, err)
	}

	saved := Credential{
		URL:    "https://example.com/mcp",
		Config: oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: "https://example.com/token"}},
		Token:  oauth2.Token{AccessToken: "access", RefreshToken: "refresh"},
	}
	if err := store.Save("work", saved); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("work")
	if err != nil || loaded.URL != saved.URL || loaded.Config.ClientID != "client" || loaded.Config.Endpoint.TokenURL != saved.Config.Endpoint.TokenURL ||
		loaded.Token.AccessToken != "access" || loaded.Token.RefreshToken != "refresh" {
		t.Fatalf("loaded credential = %+v, %v", loaded, err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("credentials directory = %v, %v", entries, err)
	}

	if info, err := entries[0].Info(); err != nil || info.Name() != "work.json" || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file = %v, %v", info, err)
	}
}
