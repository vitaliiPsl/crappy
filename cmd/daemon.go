package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vitaliipsl/crappy/internal/app"
	"github.com/vitaliipsl/crappy/internal/daemon"
	"github.com/vitaliipsl/crappy/internal/settings"
)

func daemonDirectory(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "daemon")
}

func backgroundManager(directory, settingsPath string) (*daemon.Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}

	command := []string{executable, commandServe}
	environment := backgroundEnvironment(settingsPath)

	return daemon.NewManager(directory, command, environment), nil
}

func backgroundEnvironment(settingsPath string) []string {
	prefix := settings.EnvPath + "="

	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, prefix) {
			environment = append(environment, variable)
		}
	}

	return append(environment, prefix+settingsPath)
}

func serve(ctx context.Context, configuration settings.Settings, directory string) error {
	instance, err := daemon.Acquire(directory)
	if errors.Is(err, daemon.ErrRunning) {
		return nil
	}

	if err != nil {
		return err
	}

	defer func() { _ = instance.Close() }()

	storageLock, err := daemon.AcquireLock(filepath.Join(configuration.SessionsPath, ".lock"))
	if err != nil {
		return fmt.Errorf("acquire session storage: %w", err)
	}

	defer func() { _ = storageLock.Close() }()

	runtime, err := app.Open(ctx, configuration)
	if err != nil {
		return err
	}

	listener, err := instance.Listen()
	if err != nil {
		return errors.Join(err, runtime.Shutdown(context.Background()))
	}

	defer func() { _ = listener.Close() }()

	return daemon.New(runtime.Service(), runtime.Shutdown).Run(ctx, listener)
}
