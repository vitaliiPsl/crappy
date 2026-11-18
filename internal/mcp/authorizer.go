package mcp

import (
	"errors"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

var ErrAuthorizationRequired = errors.New("authorization required")

type Authorizer interface {
	Handler(config ServerConfig) (auth.OAuthHandler, error)
}
