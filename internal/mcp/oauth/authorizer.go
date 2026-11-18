package oauth

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/vitaliipsl/crappy/internal/mcp"
)

type Option func(*Authorizer)

func WithCallback(callback *Callback) Option {
	return func(a *Authorizer) {
		a.callback = callback
	}
}

type Authorizer struct {
	store    *Store
	callback *Callback
}

func NewAuthorizer(store *Store, options ...Option) *Authorizer {
	authorizer := &Authorizer{store: store}
	for _, option := range options {
		option(authorizer)
	}

	return authorizer
}

func (a *Authorizer) Handler(config mcp.ServerConfig) (auth.OAuthHandler, error) {
	tokens, err := a.tokens(config)
	if err != nil {
		return nil, err
	}

	if a.callback == nil {
		return NewHandler(tokens), nil
	}

	return auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:              "crappy",
				RedirectURIs:            []string{a.callback.URL()},
				TokenEndpointAuthMethod: "none",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
			},
		},
		RedirectURL:              a.callback.URL(),
		RequestRefreshToken:      true,
		InitialTokenSource:       tokens,
		AuthorizationCodeFetcher: a.callback.Fetch,
		NewTokenSource: func(_ context.Context, oauthConfig *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			credential := Credential{URL: config.URL, Config: *oauthConfig, Token: *token}
			if err := a.store.Save(config.Name, credential); err != nil {
				return nil, err
			}

			return NewTokenSource(a.store, config.Name, credential), nil
		},
	})
}

func (a *Authorizer) tokens(config mcp.ServerConfig) (oauth2.TokenSource, error) {
	credential, err := a.store.Load(config.Name)
	if err != nil || credential == nil || credential.URL != config.URL {
		return nil, err
	}

	return NewTokenSource(a.store, config.Name, *credential), nil
}

type Handler struct {
	tokens oauth2.TokenSource
}

func NewHandler(tokens oauth2.TokenSource) *Handler {
	return &Handler{tokens: tokens}
}

func (h *Handler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return h.tokens, nil
}

func (h *Handler) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	_ = response.Body.Close()

	return mcp.ErrAuthorizationRequired
}
