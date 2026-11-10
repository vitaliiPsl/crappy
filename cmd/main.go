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
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/instructions"
	"github.com/vitaliipsl/crappy/internal/memory"
	"github.com/vitaliipsl/crappy/internal/model"
	"github.com/vitaliipsl/crappy/internal/settings"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools"
	"github.com/vitaliipsl/crappy/internal/tools/local"
	corememory "github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/bm25"
	jsonstore "github.com/vitaliipsl/crappy/memory/json"
	"github.com/vitaliipsl/crappy/memory/pipeline"
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

	estimator := token.NewCharacterEstimator()

	h, app, err := newAssistant(ctx, settings, config, estimator)
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

	return tui.Run(ctx, app)
}

func newAssistant(
	ctx context.Context,
	settings settings.Settings,
	config config.Config,
	estimator token.Estimator,
) (*harness.Harness, *assistant.Assistant, error) {
	models, err := model.NewRegistry(settings.Providers)
	if err != nil {
		return nil, nil, fmt.Errorf("create model registry: %w", err)
	}

	modelResolver := models.Resolver(
		config.ProviderID,
		config.ModelID,
		model.Retry(settings.Retry),
	)

	compactor, err := compaction.NewCompactor(config.CompactionConfig(), modelResolver, compaction.WithEstimator(estimator))
	if err != nil {
		return nil, nil, fmt.Errorf("create compactor: %w", err)
	}

	compactionExecutor := compactionexec.New(compactor)

	storage, err := jsonl.Open(ctx, settings.SessionsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open session storage: %w", err)
	}

	memoryStore, err := jsonstore.New(settings.MemoryPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open memory storage: %w", err)
	}

	memoryIndex, err := bm25.New(ctx, memoryStore)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize memory index: %w", err)
	}

	memoryPipeline, err := pipeline.New(memoryStore, pipeline.WithRetrievers(memoryIndex))
	if err != nil {
		return nil, nil, fmt.Errorf("create memory pipeline: %w", err)
	}

	memories := corememory.New(memoryStore,
		corememory.WithIndexer(memoryIndex, func(_ context.Context, err error) {
			log.Printf("memory indexing: %v", err)
		}),
		corememory.WithPipeline(memoryPipeline),
	)

	agents := agent.New(config.AgentConfig(), modelResolver,
		agent.WithToolMiddleware(
			tools.OutputCap(settings.Tools, storage.PutBlob),
			tools.Timeout(settings.Tools),
		),
		agent.WithContributors(
			instructions.NewContributor(),
			local.NewContributor(),
			memory.NewContributor(memories),
			automatic.New(compactor,
				automatic.WithEstimator(estimator),
				automatic.WithTrigger(
					automatic.WhenInputExceedsTokens(config.Compaction.ThresholdTokens),
				),
			),
		),
	)

	h := harness.New(storage,
		harness.WithExecutor(agent.ExecutorName, agent.NewExecutor(agents)),
		harness.WithExecutor(compactionexec.ExecutorName, compactionExecutor),
	)

	return h, assistant.New(config, h, storage, agents, estimator), nil
}
