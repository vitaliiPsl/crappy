package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/internal/client/tui"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/runtime"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/internal/tools/bash"
	"github.com/vitaliipsl/crappy/internal/tools/filesystem"
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
	runtime, err := newRuntime(ctx)
	if err != nil {
		return err
	}

	return tui.Run(ctx, runtime)
}

func newRuntime(ctx context.Context) (*runtime.Runtime, error) {
	settings, err := settings.Load()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	config, err := config.LoadFile(settings.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	models, err := model.NewRegistry(settings.Providers)
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
		agent.WithTools(bash.New()),
		agent.WithTools(filesystem.New()...),
		agent.WithToolMiddleware(tools.Timeout(settings.Tools)),
		agent.WithContributors(automatic.New()),
	)

	storage, err := jsonl.Open(ctx, settings.SessionsPath)
	if err != nil {
		return nil, fmt.Errorf("open session storage: %w", err)
	}

	return runtime.NewRuntime(
		ctx,
		storage.SessionStore(),
		storage.EntryStore(),
		agents,
		generator,
	), nil
}
