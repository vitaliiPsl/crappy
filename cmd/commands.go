package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/vitaliipsl/crappy/internal/api"
	"github.com/vitaliipsl/crappy/internal/client/tui"
	"github.com/vitaliipsl/crappy/internal/daemon"
	"github.com/vitaliipsl/crappy/internal/settings"
)

const (
	commandStatus   = "status"
	commandStop     = "stop"
	commandServe    = "--serve"
	commandMCPLogin = "mcp login"

	usage = "usage: crappy [status|stop|mcp login <server>]"
)

func run(ctx context.Context, args []string) error {
	command, err := parseCommand(args)
	if err != nil {
		return err
	}

	settingsPath, err := settings.Path()
	if err != nil {
		return err
	}

	settings, err := settings.LoadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	if command == commandMCPLogin {
		return loginServer(ctx, settings.MCP, args[2], os.Stdout)
	}

	directory := daemonDirectory(settingsPath)
	if command == commandServe {
		return serve(ctx, settings, directory)
	}

	manager, err := backgroundManager(directory, settingsPath)
	if err != nil {
		return err
	}

	switch command {
	case commandStatus:
		return showStatus(ctx, manager, os.Stdout)
	case commandStop:
		return stopDaemon(ctx, manager, os.Stdout)
	default:
		return openTerminal(ctx, manager, os.Stdout)
	}
}

func parseCommand(args []string) (string, error) {
	switch {
	case len(args) == 0:
		return "", nil
	case len(args) == 1 && slices.Contains([]string{commandStatus, commandStop, commandServe}, args[0]):
		return args[0], nil
	case len(args) == 3 && strings.Join(args[:2], " ") == commandMCPLogin:
		return commandMCPLogin, nil
	default:
		return "", errors.New(usage)
	}
}

func openTerminal(ctx context.Context, manager *daemon.Manager, output io.Writer) error {
	if err := manager.Ensure(ctx); err != nil {
		return err
	}

	directory, err := os.Getwd()
	if err != nil {
		return err
	}

	client := api.NewClient(manager.SocketPath(), directory)
	defer client.Close()

	if err := tui.Run(ctx, client, directory); err != nil {
		return err
	}

	_, err = fmt.Fprintln(output, "Disconnected. Crappy continues running. Run crappy to return.")

	return err
}

func showStatus(ctx context.Context, manager *daemon.Manager, output io.Writer) error {
	status, err := manager.Status(ctx)
	if err != nil {
		return err
	}

	if status.PID == 0 {
		_, err = fmt.Fprintln(output, "Stopped")
	} else {
		_, err = fmt.Fprintf(output, "Running (PID %d)\n", status.PID)
	}

	return err
}

func stopDaemon(ctx context.Context, manager *daemon.Manager, output io.Writer) error {
	if err := manager.Stop(ctx); err != nil {
		return err
	}

	_, err := fmt.Fprintln(output, "Crappy stopped. Interrupted work and queued messages are preserved.")

	return err
}
