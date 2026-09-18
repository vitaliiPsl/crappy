package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/internal/client/cli"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/runtime"
	"github.com/vitaliipsl/crappy/internal/settings"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	runtime, err := newRuntime()
	if err != nil {
		return err
	}

	return cli.New(
		runtime,
		uuid.NewString(),
		os.Stdin,
		os.Stdout,
	).Run(ctx)
}

func newRuntime() (*runtime.Runtime, error) {
	settings, err := settings.Load()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	config, err := config.LoadFile(settings.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	models, err := model.NewRegistryFromSettings(settings)
	if err != nil {
		return nil, fmt.Errorf("create model registry: %w", err)
	}

	modelResolver := models.Resolver(config.ProviderID, config.ModelID)

	generator, err := checkpoint.NewGenerator(modelResolver)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint generator: %w", err)
	}

	agents := agent.New(
		config.AgentConfig(),
		modelResolver,
		agent.WithContributors(automatic.New()),
	)

	return runtime.NewRuntime(
		entry.NewMemoryStore(),
		agents,
		generator,
	), nil
}
