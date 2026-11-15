package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vitaliipsl/crappy/internal/api"
)

const (
	startupTimeout = 15 * time.Second
	statusTimeout  = time.Second
	pollInterval   = 50 * time.Millisecond
)

type Manager struct {
	directory   string
	command     []string
	environment []string
}

func NewManager(directory string, command, environment []string) *Manager {
	return &Manager{
		directory:   directory,
		command:     append([]string(nil), command...),
		environment: append([]string(nil), environment...),
	}
}

func (m *Manager) SocketPath() string {
	return filepath.Join(m.directory, "socket")
}

func (m *Manager) LogPath() string {
	return filepath.Join(m.directory, "log")
}

func (m *Manager) Status(ctx context.Context) (api.Health, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	client := api.NewClient(m.SocketPath(), "")
	defer client.Close()

	value, err := client.Health(ctx)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return api.Health{}, nil
	}

	return value, err
}

func (m *Manager) Ensure(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	status, err := m.Status(ctx)
	if err == nil && status.PID != 0 {
		return nil
	}

	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	exited, err := m.startProcess()
	if err != nil {
		return err
	}

	return m.waitForReady(ctx, exited)
}

func (m *Manager) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	status, err := m.Status(ctx)
	if err != nil {
		return err
	}

	if status.PID == 0 {
		return nil
	}

	client := api.NewClient(m.SocketPath(), "")
	defer client.Close()

	if err := client.Stop(ctx); err != nil {
		return err
	}

	return m.waitForExit(ctx)
}

func (m *Manager) startProcess() (<-chan error, error) {
	if err := os.MkdirAll(m.directory, 0700); err != nil {
		return nil, err
	}

	log, err := os.OpenFile(m.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}

	defer func() { _ = log.Close() }()

	command := exec.Command(m.command[0], m.command[1:]...)
	command.Env = m.environment
	command.Dir = "/"

	command.Stdout = log

	command.Stderr = log
	if err := detach(command); err != nil {
		return nil, err
	}

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start assistant: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()

	return exited, nil
}

func (m *Manager) waitForReady(ctx context.Context, exited <-chan error) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for assistant (logs: %s): %w", m.LogPath(), ctx.Err())
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("assistant startup failed (logs: %s): %w", m.LogPath(), err)
			}

			exited = nil
		case <-ticker.C:
			status, err := m.Status(ctx)
			if err == nil && status.PID != 0 {
				return nil
			}
		}
	}
}

func (m *Manager) waitForExit(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		connection, err := (&net.Dialer{}).DialContext(ctx, "unix", m.SocketPath())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
				return nil
			}

			return err
		}

		_ = connection.Close()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
