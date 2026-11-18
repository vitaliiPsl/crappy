package oauth

import (
	"context"
	"sync"

	"golang.org/x/oauth2"
)

type TokenSource struct {
	store      *Store
	server     string
	credential Credential
	tokens     oauth2.TokenSource
	mu         sync.Mutex
}

func NewTokenSource(store *Store, server string, credential Credential) *TokenSource {
	return &TokenSource{
		store:      store,
		server:     server,
		credential: credential,
		tokens:     credential.Config.TokenSource(context.Background(), &credential.Token),
	}
}

func (s *TokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := s.tokens.Token()
	if err != nil {
		return nil, err
	}

	if token.AccessToken == s.credential.Token.AccessToken {
		return token, nil
	}

	// Providers may rotate the refresh token on every refresh, so a refreshed token that is not stored is lost on restart.
	credential := s.credential
	credential.Token = *token

	if err := s.store.Save(s.server, credential); err != nil {
		return nil, err
	}

	s.credential = credential

	return token, nil
}
