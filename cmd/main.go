package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/checkpoint"
	"github.com/vitaliipsl/crappy/harness/checkpoint/automatic"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/client/tui"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/model"
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
	h, sessions, err := newHarness(ctx)
	if err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		_ = h.Run(ctx)
	}()

	defer func() {
		_ = h.Shutdown(context.Background())

		<-done
	}()

	return tui.Run(ctx, h, sessions)
}

func newHarness(ctx context.Context) (*harness.Harness, session.Store, error) {
	settings, err := settings.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load settings: %w", err)
	}

	config, err := config.LoadFile(settings.ConfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	models, err := model.NewRegistry(settings.Providers)
	if err != nil {
		return nil, nil, fmt.Errorf("create model registry: %w", err)
	}

	modelResolver := models.Resolver(
		config.ProviderID,
		config.ModelID,
		model.Retry(settings.Retry),
	)

	generator, err := checkpoint.NewGenerator(config.CheckpointConfig(), modelResolver)
	if err != nil {
		return nil, nil, fmt.Errorf("create checkpoint generator: %w", err)
	}

	storage, err := jsonl.Open(ctx, settings.SessionsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open session storage: %w", err)
	}

	return harness.New(
		storage,
		harness.WithModel(modelResolver),
		harness.WithAgentConfig(config.AgentConfig(),
			agent.WithTools(bash.New()),
			agent.WithTools(filesystem.New()...),
			agent.WithToolMiddleware(tools.Timeout(settings.Tools)),
			agent.WithContributors(automatic.New(generator,
				automatic.WithTrigger(
					automatic.WhenInputExceedsTokens(config.Checkpoint.ThresholdTokens),
				),
			)),
		),
	), storage.SessionStore(), nil
}
