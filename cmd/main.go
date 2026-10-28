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
	"github.com/vitaliipsl/crappy/harness/compaction"
	"github.com/vitaliipsl/crappy/harness/compaction/automatic"
	compactionexec "github.com/vitaliipsl/crappy/harness/compaction/executor"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/internal/tools/local"
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
	settings, err := settings.Load()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	config, err := config.LoadFile(settings.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	h, sessions, err := newHarness(ctx, settings, config)
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

	return tui.Run(ctx, assistant.New(h), sessions, config)
}

func newHarness(ctx context.Context, settings settings.Settings, config config.Config) (*harness.Harness, state.Store, error) {
	models, err := model.NewRegistry(settings.Providers)
	if err != nil {
		return nil, nil, fmt.Errorf("create model registry: %w", err)
	}

	modelResolver := models.Resolver(
		config.ProviderID,
		config.ModelID,
		model.Retry(settings.Retry),
	)

	compactor, err := compaction.NewCompactor(config.CompactionConfig(), modelResolver)
	if err != nil {
		return nil, nil, fmt.Errorf("create compactor: %w", err)
	}

	compactionExecutor := compactionexec.New(compactor)

	storage, err := jsonl.Open(ctx, settings.SessionsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open session storage: %w", err)
	}

	return harness.New(
		storage,
		harness.WithModel(modelResolver),
		harness.WithExecutor(compactionexec.ExecutorName, compactionExecutor),
		harness.WithAgentConfig(config.AgentConfig(),
			agent.WithToolMiddleware(tools.Timeout(settings.Tools)),
			agent.WithContributors(
				local.NewContributor(),
				automatic.New(compactionExecutor,
					automatic.WithTrigger(
						automatic.WhenInputExceedsTokens(config.Compaction.ThresholdTokens),
					),
				),
			),
		),
	), storage.SessionStore(), nil
}
