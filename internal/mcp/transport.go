package mcp

import (
	"net/http"
	"os"
	"os/exec"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewTransport(config ServerConfig) mcpsdk.Transport {
	if config.Command != "" {
		command := exec.Command(config.Command, config.Args...)
		command.Stderr = os.Stderr
		command.Env = command.Environ()

		for name, value := range config.Env {
			command.Env = append(command.Env, name+"="+value)
		}

		return &mcpsdk.CommandTransport{
			Command: command,
		}
	}

	headers := make(http.Header)
	for name, value := range config.Headers {
		headers.Set(name, os.ExpandEnv(value))
	}

	return &mcpsdk.StreamableClientTransport{
		Endpoint:   config.URL,
		HTTPClient: &http.Client{
			Transport: NewHeaderTransport(headers),
		},
	}
}

type HeaderTransport struct {
	headers http.Header
}

func NewHeaderTransport(headers http.Header) *HeaderTransport {
	return &HeaderTransport{
		headers: headers,
	}
}

func (t *HeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	for name, values := range t.headers {
		if cloned.Header.Get(name) == "" {
			cloned.Header[name] = values
		}
	}

	return http.DefaultTransport.RoundTrip(cloned)
}
