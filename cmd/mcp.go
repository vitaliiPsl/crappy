package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"slices"
	"time"

	"github.com/vitaliipsl/crappy/internal/mcp"
	"github.com/vitaliipsl/crappy/internal/mcp/oauth"
)

const loginTimeout = 5 * time.Minute

func loginServer(ctx context.Context, config mcp.Config, name string, output io.Writer) error {
	index := slices.IndexFunc(config.Servers, func(server mcp.ServerConfig) bool { return server.Name == name })
	if index < 0 {
		return fmt.Errorf("MCP server %q is not configured", name)
	}

	callback, err := oauth.NewCallback(func(url string) {
		_, _ = fmt.Fprintf(output, "Opening the browser to authorize %s. If it does not open, visit:\n%s\n", name, url)
		_ = openBrowser(url)
	})
	if err != nil {
		return err
	}

	defer func() { _ = callback.Close() }()

	config.Servers = []mcp.ServerConfig{config.Servers[index]}
	config.ConnectTimeout = loginTimeout

	manager := mcp.NewManager(config, oauth.NewAuthorizer(oauth.NewStore(config.CredentialsPath), oauth.WithCallback(callback)))
	defer func() { _ = manager.Shutdown(ctx) }()

	if err := manager.Refresh(ctx); err != nil {
		return fmt.Errorf("connect to MCP server %q: %w", name, err)
	}

	_, err = fmt.Fprintf(output, "Connected to %s with %d tools. A running crappy picks it up within %s.\n",
		name, len(manager.Servers()[0].Tools), config.PollInterval)

	return err
}

func openBrowser(url string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}

	return exec.Command(command, url).Start()
}
